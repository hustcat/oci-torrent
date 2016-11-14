package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
	netcontext "golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	"github.com/hustcat/oci-torrent/api/grpc/types"
	"github.com/hustcat/oci-torrent/version"
)

const usage = `OCI image torrent cli`

type exit struct {
	Code int
}

func main() {
	// We want our defer functions to be run when calling fatal()
	defer func() {
		if e := recover(); e != nil {
			if ex, ok := e.(exit); ok == true {
				os.Exit(ex.Code)
			}
			panic(e)
		}
	}()
	app := cli.NewApp()
	app.Name = "ctr"
	if version.GitCommit != "" {
		app.Version = fmt.Sprintf("%s commit: %s", version.Version, version.GitCommit)
	} else {
		app.Version = version.Version
	}
	app.Usage = usage
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "debug",
			Usage: "enable debug output in the logs",
		},
		cli.StringFlag{
			Name:  "address",
			Value: "unix:///run/oci-torrentd/oci-torrentd.sock",
			Usage: "proto://address of GRPC API",
		},
		cli.DurationFlag{
			Name:  "conn-timeout",
			Value: 1 * time.Second,
			Usage: "GRPC connection timeout",
		},
	}
	app.Commands = []cli.Command{
		startDownloadCommand,
		stopDownloadCommand,
		statusCommand,
		versionCommand,
	}
	app.Before = func(context *cli.Context) error {
		if context.GlobalBool("debug") {
			logrus.SetLevel(logrus.DebugLevel)
		}
		return nil
	}
	if err := app.Run(os.Args); err != nil {
		logrus.Fatal(err)
	}
}

var versionCommand = cli.Command{
	Name:  "version",
	Usage: "return the daemon version",
	Action: func(context *cli.Context) {
		c := getClient(context)
		resp, err := c.GetServerVersion(netcontext.Background(), &types.GetServerVersionRequest{})
		if err != nil {
			fatal(err.Error(), 1)
		}
		fmt.Printf("daemon version %d.%d.%d commit: %s\n", resp.Major, resp.Minor, resp.Patch, resp.Revision)
	},
}

var startDownloadCommand = cli.Command{
	Name:      "start",
	Usage:     "start download",
	ArgsUsage: "IMAGE",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "quiet",
			Usage: "not attach stdio",
		},
	},
	Action: func(context *cli.Context) {
		var (
			image = context.Args().Get(0)
		)

		if image == "" {
			fatal("image cannot be empty", ExitStatusMissingArg)
		}

		s, err := createStdio()
		defer func() {
			if s.stdin != "" {
				os.RemoveAll(filepath.Dir(s.stdin))
			}
		}()
		if err != nil {
			fatal(err.Error(), 1)
		}

		if !context.Bool("quiet") {
			if err := attachStdio(s); err != nil {
				fatal(err.Error(), 1)
			}
		}
		c := getClient(context)
		_, err = c.StartDownload(netcontext.Background(), &types.StartDownloadRequest{
			Source: image,
			Stdout: s.stdout,
			Stderr: s.stderr,
		})
		if err != nil {
			fatal(err.Error(), 1)
		}
	},
}

var stopDownloadCommand = cli.Command{
	Name:      "stop",
	Usage:     "stop download",
	ArgsUsage: "IMAGE",
	Flags: []cli.Flag{
		// FIXME: remove it
		cli.StringFlag{
			Name:  "dummy",
			Value: "",
			Usage: "dummy usage",
		},
	},
	Action: func(context *cli.Context) {
		var (
			image = context.Args().Get(0)
		)

		if image == "" {
			fatal("image cannot be empty", ExitStatusMissingArg)
		}

		c := getClient(context)
		resp, err := c.StopDownload(netcontext.Background(), &types.StopDownloadRequest{
			Source: image,
		})
		if err != nil {
			fatal(err.Error(), 1)
		}

		for _, id := range resp.Ids {
			fmt.Printf("Stopped: %s\n", id)
		}
	},
}

var statusCommand = cli.Command{
	Name:      "status",
	Usage:     "status download",
	ArgsUsage: "IMAGE",
	Flags: []cli.Flag{
		// FIXME: remove it
		cli.StringFlag{
			Name:  "dummy",
			Value: "",
			Usage: "dummy usage",
		},
	},
	Action: func(context *cli.Context) {
		var (
			image = context.Args().Get(0)
		)

		if image == "" {
			fatal("image cannot be empty", ExitStatusMissingArg)
		}

		c := getClient(context)
		resp, err := c.Status(netcontext.Background(), &types.StatusRequest{
			Source: image,
		})
		if err != nil {
			fatal(err.Error(), 1)
		}

		w := tabwriter.NewWriter(os.Stdout, 20, 1, 3, ' ', 0)
		fmt.Fprintf(w, "ID\tSTATE\tCOMPLETED\tTOTALLEN\tSEEDING\n")
		for _, s := range resp.LayerDownStates {
			fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%v\n", TruncateID(s.Id), s.State, s.Completed,
				s.Size, s.Seeding)
		}
		w.Flush()
	},
}

func fatal(err string, code int) {
	fmt.Fprintf(os.Stderr, "[ctr] %s\n", err)
	panic(exit{code})
}

func getClient(ctx *cli.Context) types.APIClient {
	// Parse proto://address form addresses.
	bindSpec := ctx.GlobalString("address")
	bindParts := strings.SplitN(bindSpec, "://", 2)
	if len(bindParts) != 2 {
		fatal(fmt.Sprintf("bad bind address format %s, expected proto://address", bindSpec), 1)
	}

	// reset the logger for grpc to log to dev/null so that it does not mess with our stdio
	grpclog.SetLogger(log.New(ioutil.Discard, "", log.LstdFlags))
	dialOpts := []grpc.DialOption{grpc.WithInsecure(), grpc.WithTimeout(ctx.GlobalDuration("conn-timeout"))}
	dialOpts = append(dialOpts,
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout(bindParts[0], bindParts[1], timeout)
		},
		))
	conn, err := grpc.Dial(bindSpec, dialOpts...)
	if err != nil {
		fatal(err.Error(), 1)
	}
	return types.NewAPIClient(conn)
}
