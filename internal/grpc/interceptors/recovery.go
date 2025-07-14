package interceptors

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"letraz-utils/internal/logging"
	"letraz-utils/pkg/utils"
)

// RecoveryInterceptor returns a gRPC unary interceptor that recovers from panics
func RecoveryInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				// Generate stack trace
				stackTrace := string(debug.Stack())

				// Log the panic
				logger := logging.GetGlobalLogger()
				logger.Error("gRPC handler panic recovered", map[string]interface{}{
					"method":      info.FullMethod,
					"panic":       fmt.Sprintf("%v", r),
					"stack_trace": stackTrace,
					"type":        "grpc_panic",
				})

				// Convert panic to gRPC error
				err = status.Errorf(codes.Internal, "internal server error: %v", r)
				resp = nil
			}
		}()

		// Call the actual handler
		return handler(ctx, req)
	}
}

// StreamRecoveryInterceptor returns a gRPC streaming interceptor that recovers from panics
func StreamRecoveryInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) (err error) {
		defer func() {
			if r := recover(); r != nil {
				// Generate stack trace
				stackTrace := string(debug.Stack())

				// Log the panic
				logger := logging.GetGlobalLogger()
				logger.Error("gRPC stream handler panic recovered", map[string]interface{}{
					"method":      info.FullMethod,
					"panic":       fmt.Sprintf("%v", r),
					"stack_trace": stackTrace,
					"type":        "grpc_stream_panic",
				})

				// Convert panic to gRPC error
				err = status.Errorf(codes.Internal, "internal server error: %v", r)
			}
		}()

		// Call the actual handler
		return handler(srv, ss)
	}
}

// PanicRecoveryHandler is a customizable panic recovery handler
type PanicRecoveryHandler func(p interface{}) error

// RecoveryInterceptorWithHandler returns a gRPC unary interceptor with custom panic handler
func RecoveryInterceptorWithHandler(recoveryHandler PanicRecoveryHandler) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				// Generate stack trace
				stackTrace := string(debug.Stack())

				// Log the panic
				logger := logging.GetGlobalLogger()
				logger.Error("gRPC handler panic recovered", map[string]interface{}{
					"method":      info.FullMethod,
					"panic":       fmt.Sprintf("%v", r),
					"stack_trace": stackTrace,
					"type":        "grpc_panic",
				})

				// Use custom recovery handler if provided
				if recoveryHandler != nil {
					err = recoveryHandler(r)
				} else {
					err = status.Errorf(codes.Internal, "internal server error: %v", r)
				}
				resp = nil
			}
		}()

		// Call the actual handler
		return handler(ctx, req)
	}
}

// DefaultPanicRecoveryHandler is the default panic recovery handler
func DefaultPanicRecoveryHandler() PanicRecoveryHandler {
	return func(p interface{}) error {
		return status.Errorf(codes.Internal, "internal server error: %v", p)
	}
}

// DetailedPanicRecoveryHandler returns a recovery handler that includes stack trace in error
func DetailedPanicRecoveryHandler() PanicRecoveryHandler {
	return func(p interface{}) error {
		// Only include stack trace in development mode
		if utils.IsDevelopment() {
			pc, file, line, _ := runtime.Caller(2)
			fn := runtime.FuncForPC(pc)
			return status.Errorf(codes.Internal,
				"internal server error: %v (at %s:%d in %s)",
				p, file, line, fn.Name())
		}
		return status.Errorf(codes.Internal, "internal server error: %v", p)
	}
}
