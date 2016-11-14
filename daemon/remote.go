package daemon

import (
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"

	"github.com/hustcat/oci-torrent/api/grpc/types"
)

// TODO: parse flags and pass opts
func (daemon *Daemon) getRemotePeer(address string) types.APIClient {
	bindParts := strings.SplitN(address, "://", 2)

	dialOpts := []grpc.DialOption{grpc.WithInsecure(), grpc.WithTimeout(daemon.config.ConnTimeout)}
	dialOpts = append(dialOpts,
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout(bindParts[0], bindParts[1], timeout)
		},
		))
	conn, err := grpc.Dial(address, dialOpts...)
	if err != nil {
		return nil
	}
	return types.NewAPIClient(conn)
}
