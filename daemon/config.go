package daemon

import (
	"time"
)

type Config struct {
	Pidfile     string
	Root        string
	ConnTimeout time.Duration
	UseHardlink bool

	BtEnable          bool
	BtSeeder          bool
	BtTrackers        []string
	BtSeederServer    []string
	UploadRateLimit   int
	DownloadRateLimit int
}
