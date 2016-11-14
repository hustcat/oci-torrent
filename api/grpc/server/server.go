package server

import (
	"golang.org/x/net/context"

	"github.com/hustcat/oci-torrent/api/grpc/types"
	"github.com/hustcat/oci-torrent/daemon"
	"github.com/hustcat/oci-torrent/version"
)

type apiServer struct {
	backend *daemon.Daemon
}

func NewServer(be *daemon.Daemon) types.APIServer {
	return &apiServer{
		backend: be,
	}
}

func (s *apiServer) GetServerVersion(ctx context.Context, r *types.GetServerVersionRequest) (*types.GetServerVersionResponse, error) {
	return &types.GetServerVersionResponse{
		Major:    version.VersionMajor,
		Minor:    version.VersionMinor,
		Patch:    version.VersionPatch,
		Revision: version.GitCommit,
	}, nil
}

func (s *apiServer) StartDownload(ctx context.Context, r *types.StartDownloadRequest) (*types.StartDownloadResponse, error) {
	return s.backend.StartDownload(ctx, r)
}

func (s *apiServer) StopDownload(ctx context.Context, r *types.StopDownloadRequest) (*types.StopDownloadResponse, error) {
	return s.backend.StopDownload(ctx, r)
}

func (s *apiServer) GetTorrent(ctx context.Context, r *types.GetTorrentRequest) (*types.GetTorrentResponse, error) {
	return s.backend.GetTorrent(ctx, r)
}

func (s *apiServer) Status(ctx context.Context, r *types.StatusRequest) (*types.StatusResponse, error) {
	return s.backend.Status(ctx, r)
}
