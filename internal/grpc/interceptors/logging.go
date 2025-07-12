package interceptors

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"letraz-utils/pkg/utils"
)

// LoggingInterceptor returns a gRPC unary interceptor that logs requests and responses
func LoggingInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		startTime := time.Now()
		logger := utils.GetLogger()

		// Extract request ID from context if available
		requestID := utils.GenerateRequestID()

		// Log request start
		logger.WithFields(map[string]interface{}{
			"request_id": requestID,
			"method":     info.FullMethod,
			"type":       "grpc_request_start",
		}).Info("gRPC request started")

		// Call the actual handler
		resp, err := handler(ctx, req)

		// Calculate processing time
		processingTime := time.Since(startTime)

		// Determine status code
		statusCode := codes.OK
		if err != nil {
			if s, ok := status.FromError(err); ok {
				statusCode = s.Code()
			} else {
				statusCode = codes.Internal
			}
		}

		// Log request completion
		logFields := map[string]interface{}{
			"request_id":      requestID,
			"method":          info.FullMethod,
			"processing_time": processingTime,
			"status_code":     statusCode.String(),
			"type":            "grpc_request_complete",
		}

		if err != nil {
			logFields["error"] = err.Error()
			logger.WithFields(logFields).Error("gRPC request failed")
		} else {
			logger.WithFields(logFields).Info("gRPC request completed")
		}

		return resp, err
	}
}

// StreamLoggingInterceptor returns a gRPC streaming interceptor that logs stream operations
func StreamLoggingInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		startTime := time.Now()
		logger := utils.GetLogger()

		// Generate request ID for stream
		requestID := utils.GenerateRequestID()

		// Log stream start
		logger.WithFields(map[string]interface{}{
			"request_id": requestID,
			"method":     info.FullMethod,
			"type":       "grpc_stream_start",
		}).Info("gRPC stream started")

		// Call the actual handler
		err := handler(srv, ss)

		// Calculate processing time
		processingTime := time.Since(startTime)

		// Determine status code
		statusCode := codes.OK
		if err != nil {
			if s, ok := status.FromError(err); ok {
				statusCode = s.Code()
			} else {
				statusCode = codes.Internal
			}
		}

		// Log stream completion
		logFields := map[string]interface{}{
			"request_id":      requestID,
			"method":          info.FullMethod,
			"processing_time": processingTime,
			"status_code":     statusCode.String(),
			"type":            "grpc_stream_complete",
		}

		if err != nil {
			logFields["error"] = err.Error()
			logger.WithFields(logFields).Error("gRPC stream failed")
		} else {
			logger.WithFields(logFields).Info("gRPC stream completed")
		}

		return err
	}
}
