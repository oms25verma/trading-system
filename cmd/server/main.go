package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"trading-system/internal/config"
	"trading-system/internal/kite"
	"trading-system/internal/trading"
)

func main() {
	cfg := config.Load()

	kiteClient := kite.NewClient(cfg.KiteAPIKey, cfg.KiteAPISecret, cfg.AccessToken)
	var broker trading.Broker
	if strings.EqualFold(os.Getenv("BROKER"), "kite") {
		broker = kite.NewAdapter(kiteClient)
		log.Println("broker=kite")
	} else {
		broker = trading.NewPaperBroker()
		log.Println("broker=paper")
	}

	manager, err := trading.NewManagerWithStore(broker, trading.NewJSONStore(cfg.TradeStorePath))
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go manager.TrailStops(ctx, cfg.PollInterval)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           routes(manager, kiteClient, cfg),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("listening on %s", cfg.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func routes(manager *trading.Manager, kiteClient *kite.Client, cfg config.Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /trades", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, manager.List())
	})
	mux.HandleFunc("GET /kite/login", func(w http.ResponseWriter, r *http.Request) {
		loginURL, err := kiteClient.LoginURL()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		http.Redirect(w, r, loginURL, http.StatusFound)
	})
	mux.HandleFunc("GET /kite/callback", func(w http.ResponseWriter, r *http.Request) {
		requestToken := r.URL.Query().Get("request_token")
		if requestToken == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing request_token"})
			return
		}
		session, err := kiteClient.GenerateSession(r.Context(), requestToken)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"request_token": requestToken,
				"error":         err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"message":      "Kite access token generated. Set KITE_ACCESS_TOKEN to this value before live trading.",
			"access_token": session.AccessToken,
			"user_id":      session.UserID,
			"user_name":    session.UserName,
			"login_time":   session.LoginTime,
		})
	})
	mux.HandleFunc("POST /trades", func(w http.ResponseWriter, r *http.Request) {
		var req trading.CreateTradeRequest
		if !decode(w, r, &req) {
			return
		}
		applyCreateDefaults(&req, cfg)
		trade, err := manager.Enter(r.Context(), req)
		writeResult(w, trade, err)
	})
	mux.HandleFunc("POST /trades/import", func(w http.ResponseWriter, r *http.Request) {
		var req trading.ImportTradeRequest
		if !decode(w, r, &req) {
			return
		}
		trade, err := manager.Import(req)
		writeResult(w, trade, err)
	})
	mux.HandleFunc("POST /trades/{id}/stop-loss", func(w http.ResponseWriter, r *http.Request) {
		var req trading.StopLossRequest
		if !decode(w, r, &req) {
			return
		}
		trade, err := manager.AddStopLoss(r.Context(), r.PathValue("id"), req)
		writeResult(w, trade, err)
	})
	mux.HandleFunc("DELETE /trades/{id}/stop-loss", func(w http.ResponseWriter, r *http.Request) {
		trade, err := manager.RemoveStopLoss(r.Context(), r.PathValue("id"))
		writeResult(w, trade, err)
	})
	mux.HandleFunc("POST /trades/{id}/target", func(w http.ResponseWriter, r *http.Request) {
		var req trading.TargetRequest
		if !decode(w, r, &req) {
			return
		}
		trade, err := manager.AddTarget(r.Context(), r.PathValue("id"), req)
		writeResult(w, trade, err)
	})
	mux.HandleFunc("DELETE /trades/{id}/target", func(w http.ResponseWriter, r *http.Request) {
		trade, err := manager.RemoveTarget(r.Context(), r.PathValue("id"))
		writeResult(w, trade, err)
	})
	mux.HandleFunc("POST /trades/{id}/exit", func(w http.ResponseWriter, r *http.Request) {
		trade, err := manager.Exit(r.Context(), r.PathValue("id"))
		writeResult(w, trade, err)
	})
	return mux
}

func applyCreateDefaults(req *trading.CreateTradeRequest, cfg config.Config) {
	if req.Quantity == 0 {
		req.Quantity = cfg.DefaultQuantity
	}
	if req.Product == "" {
		req.Product = cfg.DefaultProduct
	}
	if req.MarketProtection == nil {
		req.MarketProtection = cfg.DefaultMarketProtection
	}
	if req.Protection == nil {
		return
	}
	if req.Protection.StopLossPoints == 0 {
		req.Protection.StopLossPoints = cfg.DefaultStopLossPoints
	}
	if req.Protection.TargetPoints == 0 {
		req.Protection.TargetPoints = cfg.DefaultTargetPoints
	}
	if req.Protection.SLLimitOffset == 0 {
		req.Protection.SLLimitOffset = cfg.DefaultSLLimitOffset
	}
}

func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
