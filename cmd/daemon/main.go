package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
	"github.com/docker/docker/pkg/listeners"

	"github.com/hustcat/oci-torrent/api/grpc/server"
	"github.com/hustcat/oci-torrent/api/grpc/types"
	"github.com/hustcat/oci-torrent/daemon"
	"github.com/hustcat/oci-torrent/version"
)

const (
	usage               = "OCI image torrent daemon"
	defaultRootDir      = "/data/oci-torrentd"
	defaultGRPCEndpoint = "unix:///run/oci-torrentd/oci-torrentd.sock"
)

var daemonFlags = []cli.Flag{
	cli.BoolFlag{
		Name:  "debug",
		Usage: "enable debug output in the logs",
	},
	cli.StringFlag{
		Name:  "root-dir",
		Value: defaultRootDir,
		Usage: "daemon root directory",
	},
	cli.BoolFlag{
		Name:  "disable-bt",
		Usage: "disable bittorrent",
	},
	cli.BoolFlag{
		Name:  "bt-seeder",
		Usage: "run daemon as bittorent seeder",
	},
	cli.StringSliceFlag{
		Name:  "bt-tracker",
		Usage: "bittorrent tracker URLs. Ex: http://10.10.10.10:6882/announce",
	},
	cli.StringSliceFlag{
		Name:  "seeder-addr",
		Usage: "bittorrent seeder address, proto://address",
	},
	cli.IntFlag{
		Name:  "upload-rate",
		Usage: "bittorrent upload rate limit",
	},
	cli.IntFlag{
		Name:  "download-rate",
		Usage: "bittorrent download rate limit",
	},
	cli.StringFlag{
		Name:  "listen,l",
		Value: defaultGRPCEndpoint,
		Usage: "proto://address on which the GRPC API will listen",
	},
	cli.DurationFlag{
		Name:  "conn-timeout",
		Value: 1 * time.Second,
		Usage: "GRPC connection timeout",
	},
}

// DumpStacks dumps the runtime stack.
func dumpStacks() {
	var (
		buf       []byte
		stackSize int
	)
	bufferLen := 16384
	for stackSize == len(buf) {
		buf = make([]byte, bufferLen)
		stackSize = runtime.Stack(buf, true)
		bufferLen *= 2
	}
	buf = buf[:stackSize]
	logrus.Infof("=== BEGIN goroutine stack dump ===\n%s\n=== END goroutine stack dump ===", buf)
}

func setupDumpStacksTrap() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGUSR1)
	go func() {
		for range c {
			dumpStacks()
		}
	}()
}

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{TimestampFormat: time.RFC3339Nano})
	app := cli.NewApp()
	app.Name = "oci-syncerd"
	if version.GitCommit != "" {
		app.Version = fmt.Sprintf("%s commit: %s", version.Version, version.GitCommit)
	} else {
		app.Version = version.Version
	}
	app.Usage = usage
	app.Flags = daemonFlags
	app.Before = func(context *cli.Context) error {
		setupDumpStacksTrap()
		if context.GlobalBool("debug") {
			logrus.SetLevel(logrus.DebugLevel)
		}
		return nil
	}

	app.Action = func(context *cli.Context) {
		if err := runDaemon(context); err != nil {
			logrus.Fatal(err)
		}
	}
	if err := app.Run(os.Args); err != nil {
		logrus.Fatal(err)
	}
}

func runDaemon(context *cli.Context) error {
	config := &daemon.Config{
		Root:              context.String("root-dir"),
		ConnTimeout:       context.Duration("conn-timeout"),
		BtEnable:          !context.Bool("disable-bt"),
		BtSeeder:          context.Bool("bt-seeder"),
		BtTrackers:        context.StringSlice("bt-tracker"),
		BtSeederServer:    context.StringSlice("seeder-addr"),
		UploadRateLimit:   context.Int("upload-rate"),
		DownloadRateLimit: context.Int("download-rate"),
	}
	s := make(chan os.Signal, 2048)
	signal.Notify(s, syscall.SIGTERM, syscall.SIGINT)
	be, err := daemon.NewDaemon(config)
	if err != nil {
		return err
	}

	// Split the listen string of the form proto://addr
	listenSpec := context.String("listen")
	listenParts := strings.SplitN(listenSpec, "://", 2)
	if len(listenParts) != 2 {
		return fmt.Errorf("bad listen address format %s, expected proto://address", listenSpec)
	}
	server, err := startServer(listenParts[0], listenParts[1], be)
	if err != nil {
		return err
	}
	for ss := range s {
		switch ss {
		default:
			logrus.Infof("stopping server after receiving %s", ss)
			server.Stop()
			os.Exit(0)
		}
	}
	return nil
}

func startServer(protocol, address string, be *daemon.Daemon) (*grpc.Server, error) {
	sockets, err := listeners.Init(protocol, address, "", nil)
	if err != nil {
		return nil, err
	}
	if len(sockets) != 1 {
		return nil, fmt.Errorf("incorrect number of listeners")
	}
	l := sockets[0]
	s := grpc.NewServer()
	types.RegisterAPIServer(s, server.NewServer(be))

	go func() {
		logrus.Debugf("containerd: grpc api on %s", address)
		if err := s.Serve(l); err != nil {
			logrus.WithField("error", err).Fatal("containerd: serve grpc")
		}
	}()
	return s, nil
}
