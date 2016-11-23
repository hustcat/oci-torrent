package daemon

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"syscall"

	log "github.com/Sirupsen/logrus"
	distdigests "github.com/docker/distribution/digest"
	"golang.org/x/net/context"

	"github.com/containers/image/docker/reference"
	"github.com/containers/image/signature"
	"github.com/containers/image/transports"
	imagetypes "github.com/containers/image/types"

	"github.com/hustcat/oci-torrent/api/grpc/types"
	"github.com/hustcat/oci-torrent/bt"
	"github.com/hustcat/oci-torrent/utils"
)

const (
	usernameKey = "username"
	passwordKey = "password"
)

type Daemon struct {
	config *Config
	// BT engine
	btEngine *bt.BtEngine
}

func NewDaemon(config *Config) (*Daemon, error) {
	log.Debugf("Demon config: %#v", config)

	btRoot := path.Join(config.Root, "bt")
	if err := os.MkdirAll(btRoot, 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}

	ociRoot := path.Join(config.Root, "oci")
	if err := os.MkdirAll(ociRoot, 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}

	c := &bt.Config{
		DisableEncryption: true,
		EnableUpload:      true,
		EnableSeeding:     true,
		IncomingPort:      50007,
		UploadRateLimit:   config.UploadRateLimit,
		DownloadRateLimit: config.DownloadRateLimit,
	}
	btEngine := bt.NewBtEngine(btRoot, config.BtTrackers, c)
	if config.BtEnable {
		if err := btEngine.Run(); err != nil {
			return nil, fmt.Errorf("Start bt engine failed: %v", err)
		}
		log.Debugf("Start bt engine succss")
	}

	daemon := &Daemon{
		config:   config,
		btEngine: btEngine,
	}
	return daemon, nil
}

func (daemon *Daemon) StartDownload(ctx context.Context, r *types.StartDownloadRequest) (*types.StartDownloadResponse, error) {
	if r.Username != "" && r.Password != "" {
		context.WithValue(ctx, usernameKey, r.Username)
		context.WithValue(ctx, passwordKey, r.Password)
	}

	var (
		reportWriter io.WriteCloser
		err          error
	)
	if r.Stdout != "" {
		reportWriter, err = os.OpenFile(r.Stdout, syscall.O_WRONLY, 0)
		if err != nil {
			return nil, err
		}
		defer func() {
			reportWriter.Close()
		}()
	}
	writeReport := func(f string, a ...interface{}) {
		if reportWriter != nil {
			fmt.Fprintf(reportWriter, f, a...)
		}
	}

	if daemon.config.BtSeeder {
		return daemon.startSeederDownload(ctx, r.Source, reportWriter, writeReport)
	} else {
		return daemon.startLeecherDownload(ctx, r.Source, reportWriter, writeReport)
	}
}

func (daemon *Daemon) startSeederDownload(ctx context.Context, source string, reportWriter io.Writer, writeReport func(f string, a ...interface{})) (*types.StartDownloadResponse, error) {
	sysCtx := daemon.getSystemContext(ctx)

	imageSource := source
	if imageSource == "" {
		return nil, fmt.Errorf("Image source can't be nil")
	}

	srcRef, err := transports.ParseImageName(imageSource)
	if err != nil {
		return nil, fmt.Errorf("Invalid source name %s: %v", imageSource, err)
	}

	writeReport("Get layer info %s\n", imageSource)
	img, err := srcRef.NewImage(sysCtx)
	if err != nil {
		return nil, fmt.Errorf("Error new image %v", err)
	}

	layerInfos := img.LayerInfos()
	log.Debugf("layerInfos: %v", layerInfos)

	src, err := srcRef.NewImageSource(sysCtx, ociSupportedManifestMIMETypes())
	if err != nil {
		return nil, fmt.Errorf("Error initializing source %s: %v", transports.ImageName(srcRef), err)
	}

	ociImg, err := newOciImage(daemon, srcRef)
	if err != nil {
		return nil, err
	}
	defer ociImg.Close()

	for _, layer := range layerInfos {
		var ok bool
		if ok, err = ociImg.layout.Exist(ctx, layer.Digest); err != nil {
			return nil, fmt.Errorf("Error check OCI dest blob exist: %v", err)
		}

		if ok {
			// Layer exist, skip it
			writeReport("%s exist, skip it\n", layer.Digest)
			log.Infof("Layer %s exist, skip it", layer.Digest)
			continue
		}

		writeReport("Copying layer %s\n", layer.Digest)
		if err = daemon.copyLayer(ctx, ociImg, src, layer, reportWriter); err != nil {
			log.Errorf("Error copy layer %s: %v", layer.Digest, err)
			return nil, err
		} else {
			log.Infof("Success copy layer %s", layer.Digest)
		}

		log.Debugf("Start seeding layer %s", layer.Digest)
		err = daemon.startSeedingLayer(ctx, ociImg, layer.Digest, writeReport)
		if err != nil {
			return nil, err
		}
	}

	return &types.StartDownloadResponse{}, nil
}

func (daemon *Daemon) copyLayer(ctx context.Context, ociImg *OciImage, src imagetypes.ImageSource,
	srcInfo imagetypes.BlobInfo, reportWriter io.Writer) error {
	srcStream, _, err := src.GetBlob(srcInfo.Digest)
	if err != nil {
		return err
	}
	defer srcStream.Close()

	if reportWriter != nil {
		bar := utils.NewProgressBar(int(srcInfo.Size), reportWriter)
		bar.Start()

		srcStream = bar.NewProxyReader(srcStream)
		defer fmt.Fprint(reportWriter, "\n")
	}

	digest, _, err := ociImg.layout.PutBlob(ctx, srcStream)
	if err != nil {
		return fmt.Errorf("Error writing blob: %v", err)
	}

	if digest != srcInfo.Digest {
		return fmt.Errorf("Digest not match, src: %s, dest: %s", srcInfo.Digest, digest)
	}
	return nil
}

func (daemon *Daemon) startSeedingLayer(ctx context.Context, ociImg *OciImage, digest string, writeReport func(f string, a ...interface{})) error {
	id := distdigests.Digest(digest).Hex()
	// Write layer to file
	fn := daemon.btEngine.GetFilePath(id)
	layerFile, err := os.Create(fn)
	if err != nil {
		return fmt.Errorf("Create layer file %s failed: %v", fn, err)
	}
	defer layerFile.Close()

	// Copy from OCI directory
	srcFile, err := ociImg.layout.GetBlob(ctx, digest)
	if err != nil {
		return fmt.Errorf("Open oci layer %s error: %v", digest, err)
	}
	defer srcFile.Close()

	_, err = io.Copy(layerFile, srcFile)
	if err != nil {
		return fmt.Errorf("Copy oci layer %s error: %v", digest, err)
	}

	writeReport("Start seeding %s\n", id)
	// Seed layer file
	if err = daemon.btEngine.StartSeed(id); err != nil {
		log.Errorf("Seed layer %s failed: %v", id, err)
	} else {
		log.Infof("Seed layer %s success", id)
	}

	writeReport("Start seeding %s success\n", id)
	return nil
}

func (daemon *Daemon) startLeecherDownload(ctx context.Context, source string, reportWriter io.Writer, writeReport func(f string, a ...interface{})) (*types.StartDownloadResponse, error) {
	sysCtx := daemon.getSystemContext(ctx)

	imageSource := source
	if imageSource == "" {
		return nil, fmt.Errorf("Image source cannot be empty")
	}

	srcRef, err := transports.ParseImageName(imageSource)
	if err != nil {
		return nil, fmt.Errorf("Invalid source name %s: %v", imageSource, err)
	}

	writeReport("Get layer info %s\n", imageSource)
	img, err := srcRef.NewImage(sysCtx)
	if err != nil {
		return nil, fmt.Errorf("Error new image %v", err)
	}
	layerInfos := img.LayerInfos()
	log.Debugf("layerInfos: %v", layerInfos)

	ociImg, err := newOciImage(daemon, srcRef)
	if err != nil {
		return nil, err
	}
	defer ociImg.Close()

	writeReport("Start download image: %s\n", imageSource)
	for _, layer := range layerInfos {
		var ok bool
		if ok, err = ociImg.layout.Exist(ctx, layer.Digest); err != nil {
			return nil, fmt.Errorf("Error check OCI dest blob exist: %v", err)
		}

		if ok {
			// Layer exist, skip it
			writeReport("%s exist, skip it\n", layer.Digest)
			log.Infof("Layer %s exist, skip it", layer.Digest)
			continue
		}

		err = daemon.startLeechingLayer(ctx, ociImg, srcRef, layer, writeReport, reportWriter)
		if err != nil {
			// FIXME: download from image source
			return nil, err
		}
	}

	// Pull image config
	srcInfo := img.ConfigInfo()
	if srcInfo.Digest != "" {
		writeReport("Copying config %s\n", srcInfo.Digest)
		configBlob, err := img.ConfigBlob()
		if err != nil {
			return nil, err
		}

		digest, _, err := ociImg.layout.PutBlob(ctx, bytes.NewReader(configBlob))
		if err != nil {
			return nil, err
		}

		if digest != srcInfo.Digest {
			return nil, fmt.Errorf("Error config blob %s changed to %s", srcInfo.Digest, digest)
		}
	} else {
		log.Infof("Config of %s is empty", imageSource)
	}

	// Pull manifest
	manifest, _, err := img.Manifest()
	if err != nil {
		return nil, fmt.Errorf("Error reading manifest: %v", err)
	}

	writeReport("Writing manifest to image destination\n")
	if err = daemon.putManifest(ctx, ociImg, manifest); err != nil {
		return nil, fmt.Errorf("Error writing manifest: %v", err)
	}

	return &types.StartDownloadResponse{}, nil
}

func (daemon *Daemon) startLeechingLayer(ctx context.Context, ociImg *OciImage, ref imagetypes.ImageReference, layer imagetypes.BlobInfo, writeReport func(f string, a ...interface{}), reportWriter io.Writer) error {
	id := distdigests.Digest(layer.Digest).Hex()

	log.Debugf("Start leeching layer %s", id)
	writeReport("%s: Get torrent data from seeder\n", id)
	t, err := daemon.getTorrentFromSeeder(id)
	if err != nil {
		log.Errorf("Get torrent data from seeder for %s failed: %v", id, err)
		return err
	}

	var progress *bt.ProgressDownload
	if reportWriter != nil {
		progress = bt.NewProgressDownload(id, int(layer.Size), reportWriter)
	}
	// Download layer file
	if err := daemon.btEngine.StartLeecher(id, t, progress); err != nil {
		log.Errorf("Download layer %s failed: %v", id, err)
		return err
	} else {
		log.Infof("Download layer %s success", id)
	}

	// Copy to OCI directory
	writeReport("%s: Copy to OCI directory\n", id)

	fn := daemon.btEngine.GetFilePath(id)
	layerFile, err := os.Open(fn)
	if err != nil {
		return fmt.Errorf("Open layer file %s failed: %v", fn, err)
	}
	defer layerFile.Close()

	digest, _, err := ociImg.layout.PutBlob(ctx, layerFile)
	if err != nil {
		return fmt.Errorf("Error to put blob %s: %v", layer.Digest, err)
	}

	if digest != layer.Digest {
		return fmt.Errorf("Digest not match, src: %s, dest: %s", layer.Digest, digest)
	}
	return nil
}

func (daemon *Daemon) StopDownload(ctx context.Context, r *types.StopDownloadRequest) (*types.StopDownloadResponse, error) {
	imageSource := r.Source
	if imageSource == "" {
		return nil, fmt.Errorf("Image source cannot be empty")
	}

	ref, err := daemon.buildNamedTagged(imageSource)
	if err != nil {
		return nil, err
	}

	ociImg, err := newOciImageSimple(daemon, ref)
	if err != nil {
		return nil, err
	}
	defer ociImg.Close()

	log.Debugf("Stop oci image %s", imageSource)
	layers, err := daemon.getOciImageLayers(ctx, ociImg)
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, layer := range layers {
		id := distdigests.Digest(layer.digest).Hex()
		// Stop download layer file
		if err = daemon.btEngine.StopTorrent(id); err != nil {
			// FIXME: return failed layer info to client
			log.Errorf("Stop torrent %s failed: %v", id, err)
			return nil, err
		} else {
			log.Infof("Stop torrent %s success", id)
			ids = append(ids, id)
		}

		if r.Clean {
			if err = daemon.btEngine.DeleteTorrent(id); err != nil {
				log.Errorf("Delete torrent %s error: %v", id, err)
			} else {
				log.Infof("Delete torrent %s success", id)
			}
		}
	}

	return &types.StopDownloadResponse{
		Ids: ids,
	}, nil
}

func (daemon *Daemon) Status(ctx context.Context, r *types.StatusRequest) (*types.StatusResponse, error) {
	imageSource := r.Source
	if imageSource == "" {
		return nil, fmt.Errorf("Image source cannot be empty")
	}

	ref, err := daemon.buildNamedTagged(imageSource)
	if err != nil {
		return nil, err
	}

	ociImg, err := newOciImageSimple(daemon, ref)
	if err != nil {
		return nil, err
	}
	defer ociImg.Close()

	log.Debugf("Status oci image %s", imageSource)
	layers, err := daemon.getOciImageLayers(ctx, ociImg)
	if err != nil {
		return nil, err
	}

	var lss []*types.LayerDownState
	for _, layer := range layers {
		var ls *types.LayerDownState

		id := distdigests.Digest(layer.digest).Hex()
		s, err := daemon.btEngine.GetStatus(id)
		if err != nil {
			if err != bt.ErrIdNotExist {
				return nil, err
			} else {
				ls = &types.LayerDownState{
					Id:        id,
					State:     bt.Dropped.String(),
					Completed: layer.size,
					Size:      layer.size,
					Seeding:   false,
				}
			}
		} else {
			ls = &types.LayerDownState{
				Id:        id,
				State:     s.State,
				Completed: s.Completed,
				Size:      s.TotalLen,
				Seeding:   s.Seeding,
			}
		}
		lss = append(lss, ls)
	}
	return &types.StatusResponse{
		LayerDownStates: lss,
	}, nil
}

func (daemon *Daemon) GetTorrent(ctx context.Context, r *types.GetTorrentRequest) (*types.GetTorrentResponse, error) {
	t, err := daemon.btEngine.GetTorrent(r.Id)
	if err != nil {
		return nil, err
	}
	return &types.GetTorrentResponse{
		Torrent: t,
	}, nil
}

func (daemon *Daemon) getTorrentFromSeeder(id string) ([]byte, error) {
	if len(daemon.config.BtSeederServer) < 1 {
		return nil, fmt.Errorf("Seeder server cannot be empty")
	}

	// FIXME: round-robin seeder
	cli := daemon.getRemotePeer(daemon.config.BtSeederServer[0])
	r := &types.GetTorrentRequest{
		Id: id,
	}
	resp, err := cli.GetTorrent(context.Background(), r)
	if err != nil {
		return nil, err
	}

	return resp.Torrent, nil
}

// getPolicyContext handles the global "policy" flag.
func (daemon *Daemon) getPolicyContext() (*signature.PolicyContext, error) {
	policy, err := signature.DefaultPolicy(nil)
	if err != nil {
		return nil, err
	}
	return signature.NewPolicyContext(policy)
}

func (daemon *Daemon) getSystemContext(ctx context.Context) (sysCtx *imagetypes.SystemContext) {
	var (
		username string
		password string
		ok       bool
	)

	// FIXME: get value from daemon.config
	sysCtx = &imagetypes.SystemContext{
		RegistriesDirPath:           "",
		DockerCertPath:              "",
		DockerInsecureSkipTLSVerify: true,
	}

	if username, ok = ctx.Value(usernameKey).(string); !ok {
		return
	}
	if password, ok = ctx.Value(passwordKey).(string); !ok {
		return
	}

	auth := &imagetypes.DockerAuthConfig{
		Username: username,
		Password: password,
	}
	sysCtx.DockerAuthConfig = auth
	return
}

func (daemon *Daemon) buildNamedTagged(source string) (reference.Named, error) {
	n, err := reference.ParseNamed(source)
	if err != nil {
		return nil, err
	} else {
		nt := reference.WithDefaultTag(n)
		return nt, nil
	}
}

func (daemon *Daemon) btRootDir() string {
	return path.Join(daemon.config.Root, "bt")
}
