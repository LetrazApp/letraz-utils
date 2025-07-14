package server

import (
	"net"
	"time"

	letrazv1 "letraz-utils/api/proto/letraz/v1"
	"letraz-utils/internal/background"
	"letraz-utils/internal/config"
	"letraz-utils/internal/grpc/interceptors"
	"letraz-utils/internal/llm"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/logging/types"
	"letraz-utils/internal/scraper/workers"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

type Server struct {
	cfg         *config.Config
	poolManager *workers.PoolManager
	llmManager  *llm.Manager
	taskManager background.TaskManager
	logger      types.Logger
	grpcServer  *grpc.Server

	// Embed the unimplemented server methods
	letrazv1.UnimplementedScraperServiceServer
	letrazv1.UnimplementedResumeServiceServer
}

func NewServer(cfg *config.Config, poolManager *workers.PoolManager, llmManager *llm.Manager, taskManager background.TaskManager) *Server {
	return &Server{
		cfg:         cfg,
		poolManager: poolManager,
		llmManager:  llmManager,
		taskManager: taskManager,
		logger:      logging.GetGlobalLogger(),
	}
}

func (s *Server) Start(lis net.Listener) error {
	s.grpcServer = grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    30 * time.Second,
			Timeout: 5 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.MaxRecvMsgSize(32*1024*1024), // 32MB
		grpc.MaxSendMsgSize(32*1024*1024), // 32MB
		grpc.ChainUnaryInterceptor(
			interceptors.RecoveryInterceptor(),
			interceptors.LoggingInterceptor(),
			interceptors.MetricsInterceptor(),
		),
		grpc.ChainStreamInterceptor(
			interceptors.StreamRecoveryInterceptor(),
			interceptors.StreamLoggingInterceptor(),
			interceptors.StreamMetricsInterceptor(),
		),
	)

	// Register services
	letrazv1.RegisterScraperServiceServer(s.grpcServer, s)
	letrazv1.RegisterResumeServiceServer(s.grpcServer, s)

	// Enable reflection for debugging
	reflection.Register(s.grpcServer)

	s.logger.Info("Starting gRPC server", map[string]interface{}{
		"address": lis.Addr().String(),
	})

	return s.grpcServer.Serve(lis)
}

func (s *Server) Stop() {
	s.logger.Info("Shutting down gRPC server...", map[string]interface{}{})
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
}

func (s *Server) GetConfig() *config.Config {
	return s.cfg
}

func (s *Server) GetPoolManager() *workers.PoolManager {
	return s.poolManager
}

func (s *Server) GetLLMManager() *llm.Manager {
	return s.llmManager
}

func (s *Server) GetLogger() types.Logger {
	return s.logger
}
