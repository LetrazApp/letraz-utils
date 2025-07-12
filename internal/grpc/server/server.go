package server

import (
	"net"
	"time"

	letrazv1 "letraz-utils/api/proto/letraz/v1"
	"letraz-utils/internal/background"
	"letraz-utils/internal/config"
	"letraz-utils/internal/grpc/interceptors"
	"letraz-utils/internal/llm"
	"letraz-utils/internal/scraper/workers"
	"letraz-utils/pkg/utils"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

type Server struct {
	cfg         *config.Config
	poolManager *workers.PoolManager
	llmManager  *llm.Manager
	taskManager background.TaskManager
	logger      *logrus.Logger

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
		logger:      utils.GetLogger(),
	}
}

func (s *Server) Start(lis net.Listener) error {
	grpcServer := grpc.NewServer(
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
	letrazv1.RegisterScraperServiceServer(grpcServer, s)
	letrazv1.RegisterResumeServiceServer(grpcServer, s)

	// Enable reflection for debugging
	reflection.Register(grpcServer)

	s.logger.WithField("address", lis.Addr().String()).Info("Starting gRPC server")

	return grpcServer.Serve(lis)
}

func (s *Server) Stop() {
	s.logger.Info("Shutting down gRPC server...")
	// Graceful shutdown logic can be added here
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

func (s *Server) GetLogger() *logrus.Logger {
	return s.logger
}
