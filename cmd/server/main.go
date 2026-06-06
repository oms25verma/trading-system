package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"trading-system/internal/config"
	"trading-system/internal/kite"
	"trading-system/internal/observability"
	"trading-system/internal/trading"
)

func main() {
	cfg := config.Load()
	logger := newLogger(cfg)
	slog.SetDefault(logger)

	brokerName := os.Getenv("BROKER")
	if brokerName == "" {
		brokerName = "paper"
	}
	if err := cfg.Validate(brokerName); err != nil {
		logger.Error("config_invalid", "error", err)
		os.Exit(1)
	}

	kiteClient := kite.NewClient(cfg.KiteAPIKey, cfg.KiteAPISecret, cfg.AccessToken)
	var broker trading.Broker
	if strings.EqualFold(brokerName, "kite") {
		broker = kite.NewAdapter(kiteClient)
	} else {
		broker = trading.NewPaperBroker()
	}
	logger.Info("broker_selected", "broker", brokerName)

	manager, err := trading.NewManagerWithAllStores(broker, trading.NewJSONStore(cfg.TradeStorePath), trading.NewJSONOrderStore(cfg.TradeStorePath), trading.NewJSONPositionStore(cfg.TradeStorePath))
	if err != nil {
		logger.Error("manager_init_failed", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go manager.TrailStops(ctx, cfg.PollInterval)
	if cfg.SyncPollInterval > 0 {
		logger.Info("kite_sync_poller_enabled", "interval_seconds", int(cfg.SyncPollInterval.Seconds()))
		go manager.SyncKiteLoop(ctx, cfg.SyncPollInterval)
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           requestIDMiddleware(routes(manager, kiteClient, cfg), logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Info("server_listening", "addr", cfg.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server_failed", "error", err)
		os.Exit(1)
	}
}

func requestIDMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, requestID := observability.EnsureRequestID(r.Context(), r.Header.Get("X-Request-ID"))
		w.Header().Set("X-Request-ID", requestID)

		startedAt := time.Now()
		logger.InfoContext(ctx, "http_request_started",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
		)
		next.ServeHTTP(w, r.WithContext(ctx))
		logger.InfoContext(ctx, "http_request_finished",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
	})
}

func newLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

func routes(manager *trading.Manager, kiteClient *kite.Client, cfg config.Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /dashboard", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, manager.DashboardSummary())
	})
	mux.HandleFunc("GET /trades", func(w http.ResponseWriter, r *http.Request) {
		trades := manager.List()
		if !hasListQuery(r) {
			writeJSON(w, http.StatusOK, trades)
			return
		}
		writePagedResult(r.Context(), w, r, filterTrades(trades, r))
	})
	mux.HandleFunc("GET /trades/{id}", func(w http.ResponseWriter, r *http.Request) {
		trade, err := manager.GetTrade(r.PathValue("id"))
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("GET /groups", func(w http.ResponseWriter, r *http.Request) {
		groups := manager.ListGroups()
		if !hasListQuery(r) {
			writeJSON(w, http.StatusOK, groups)
			return
		}
		writePagedResult(r.Context(), w, r, filterGroups(groups, r))
	})
	mux.HandleFunc("GET /groups/{id}", func(w http.ResponseWriter, r *http.Request) {
		group, err := manager.GetGroup(r.PathValue("id"))
		writeResult(r.Context(), w, group, err)
	})
	mux.HandleFunc("GET /conflicts", func(w http.ResponseWriter, r *http.Request) {
		groups := manager.ListConflicts()
		if !hasListQuery(r) {
			writeJSON(w, http.StatusOK, groups)
			return
		}
		writePagedResult(r.Context(), w, r, filterGroups(groups, r))
	})
	mux.HandleFunc("GET /orders", func(w http.ResponseWriter, r *http.Request) {
		orders := manager.ListSyncedOrders()
		if !hasListQuery(r) {
			writeJSON(w, http.StatusOK, orders)
			return
		}
		writePagedResult(r.Context(), w, r, filterOrders(orders, r))
	})
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		order, err := manager.GetSyncedOrder(r.PathValue("id"))
		writeResult(r.Context(), w, order, err)
	})
	mux.HandleFunc("GET /positions", func(w http.ResponseWriter, r *http.Request) {
		positions := manager.ListSyncedPositions()
		if !hasListQuery(r) {
			writeJSON(w, http.StatusOK, positions)
			return
		}
		writePagedResult(r.Context(), w, r, filterPositions(positions, r))
	})
	mux.HandleFunc("GET /positions/{id}", func(w http.ResponseWriter, r *http.Request) {
		position, err := manager.GetSyncedPosition(r.PathValue("id"))
		writeResult(r.Context(), w, position, err)
	})
	mux.HandleFunc("POST /sync/kite", func(w http.ResponseWriter, r *http.Request) {
		result, err := manager.SyncKite(r.Context())
		writeResult(r.Context(), w, result, err)
	})
	mux.HandleFunc("GET /kite/login", func(w http.ResponseWriter, r *http.Request) {
		loginURL, err := kiteClient.LoginURL()
		if err != nil {
			writeError(r.Context(), w, err)
			return
		}
		http.Redirect(w, r, loginURL, http.StatusFound)
	})
	mux.HandleFunc("GET /kite/callback", func(w http.ResponseWriter, r *http.Request) {
		requestToken := r.URL.Query().Get("request_token")
		if requestToken == "" {
			writeError(r.Context(), w, &trading.DomainError{Kind: trading.ErrorKindValidation, Code: "missing_request_token", Message: "missing request_token"})
			return
		}
		session, err := kiteClient.GenerateSession(r.Context(), requestToken)
		if err != nil {
			writeError(r.Context(), w, err)
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
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("POST /trades/import", func(w http.ResponseWriter, r *http.Request) {
		var req trading.ImportTradeRequest
		if !decode(w, r, &req) {
			return
		}
		trade, err := manager.Import(r.Context(), req)
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("POST /trades/{id}/stop-loss", func(w http.ResponseWriter, r *http.Request) {
		var req trading.StopLossRequest
		if !decode(w, r, &req) {
			return
		}
		trade, err := manager.AddStopLoss(r.Context(), r.PathValue("id"), req)
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("DELETE /trades/{id}/stop-loss", func(w http.ResponseWriter, r *http.Request) {
		trade, err := manager.RemoveStopLoss(r.Context(), r.PathValue("id"))
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("POST /trades/{id}/target", func(w http.ResponseWriter, r *http.Request) {
		var req trading.TargetRequest
		if !decode(w, r, &req) {
			return
		}
		trade, err := manager.AddTarget(r.Context(), r.PathValue("id"), req)
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("DELETE /trades/{id}/target", func(w http.ResponseWriter, r *http.Request) {
		trade, err := manager.RemoveTarget(r.Context(), r.PathValue("id"))
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("POST /trades/{id}/exit", func(w http.ResponseWriter, r *http.Request) {
		trade, err := manager.Exit(r.Context(), r.PathValue("id"))
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("POST /trades/{id}/cancel-entry", func(w http.ResponseWriter, r *http.Request) {
		trade, err := manager.CancelEntry(r.Context(), r.PathValue("id"))
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("POST /trades/{id}/product-conversion/apply", func(w http.ResponseWriter, r *http.Request) {
		trade, err := manager.ApplyDetectedProductConversion(r.Context(), r.PathValue("id"))
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("POST /groups/{id}/stop-loss", func(w http.ResponseWriter, r *http.Request) {
		var req trading.StopLossRequest
		if !decode(w, r, &req) {
			return
		}
		trade, err := manager.AddGroupStopLoss(r.Context(), r.PathValue("id"), req)
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("DELETE /groups/{id}/stop-loss", func(w http.ResponseWriter, r *http.Request) {
		trade, err := manager.RemoveGroupStopLoss(r.Context(), r.PathValue("id"))
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("POST /groups/{id}/target", func(w http.ResponseWriter, r *http.Request) {
		var req trading.TargetRequest
		if !decode(w, r, &req) {
			return
		}
		trade, err := manager.AddGroupTarget(r.Context(), r.PathValue("id"), req)
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("DELETE /groups/{id}/target", func(w http.ResponseWriter, r *http.Request) {
		trade, err := manager.RemoveGroupTarget(r.Context(), r.PathValue("id"))
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("POST /groups/{id}/external-exit/link", func(w http.ResponseWriter, r *http.Request) {
		var req trading.ExternalExitLinkRequest
		if !decode(w, r, &req) {
			return
		}
		trade, err := manager.LinkGroupExternalExitOrder(r.Context(), r.PathValue("id"), req)
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("DELETE /groups/{id}/external-exit/link", func(w http.ResponseWriter, r *http.Request) {
		var req trading.ExternalExitLinkRequest
		if !decode(w, r, &req) {
			return
		}
		trade, err := manager.UnlinkGroupExternalExitOrder(r.Context(), r.PathValue("id"), req)
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("POST /groups/{id}/exit", func(w http.ResponseWriter, r *http.Request) {
		trade, err := manager.ExitGroup(r.Context(), r.PathValue("id"))
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("POST /groups/{id}/cancel-entry", func(w http.ResponseWriter, r *http.Request) {
		trade, err := manager.CancelGroupEntry(r.Context(), r.PathValue("id"))
		writeResult(r.Context(), w, trade, err)
	})
	mux.HandleFunc("POST /groups/{id}/take-over", func(w http.ResponseWriter, r *http.Request) {
		var req trading.TakeOverGroupRequest
		if !decodeOptional(w, r, &req) {
			return
		}
		trade, err := manager.TakeOverGroup(r.Context(), r.PathValue("id"), req)
		writeResult(r.Context(), w, trade, err)
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
		writeJSON(w, http.StatusBadRequest, apiError{
			Kind:    string(trading.ErrorKindValidation),
			Code:    "invalid_json",
			Message: err.Error(),
		})
		return false
	}
	return true
}

func decodeOptional(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	if r.Body == http.NoBody {
		return true
	}
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return true
		}
		writeJSON(w, http.StatusBadRequest, apiError{
			Kind:    string(trading.ErrorKindValidation),
			Code:    "invalid_json",
			Message: err.Error(),
		})
		return false
	}
	return true
}

func writeResult(ctx context.Context, w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeError(ctx, w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

type apiError struct {
	Kind    string `json:"kind"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(ctx context.Context, w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	response := apiError{
		Kind:    "BROKER",
		Code:    "broker_error",
		Message: err.Error(),
	}

	var domainErr *trading.DomainError
	if errors.As(err, &domainErr) {
		response.Kind = string(domainErr.Kind)
		response.Code = domainErr.Code
		response.Message = domainErr.Message
		status = statusForDomainError(domainErr.Kind)
	}

	slog.WarnContext(ctx, "api_error",
		"request_id", observability.RequestID(ctx),
		"status", status,
		"kind", response.Kind,
		"code", response.Code,
		"message", response.Message,
	)
	writeJSON(w, status, response)
}

func statusForDomainError(kind trading.ErrorKind) int {
	switch kind {
	case trading.ErrorKindValidation:
		return http.StatusBadRequest
	case trading.ErrorKindNotFound:
		return http.StatusNotFound
	case trading.ErrorKindConflict:
		return http.StatusConflict
	case trading.ErrorKindClosed:
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
