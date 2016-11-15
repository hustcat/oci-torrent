package daemon

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"syscall"

	log "github.com/Sirupsen/logrus"
	distdigests "github.com/docker/distribution/digest"
	"golang.org/x/net/context"

	"github.com/containers/image/copy"
	"github.com/containers/image/docker/reference"
	"github.com/containers/image/signature"
	"github.com/containers/image/transports"
	imagetypes "github.com/containers/image/types"

	"github.com/hustcat/oci-torrent/api/grpc/types"
	"github.com/hustcat/oci-torrent/bt"
)

const (
	usernameKey = "username"
	passwordKey = "password"
)

type Daemon struct {
	config *Config
	// bt
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

	if daemon.config.BtSeeder {
		return daemon.startSeederDownload(ctx, r)
	} else {
		return daemon.startLeecherDownload(ctx, r)
	}
}

func (daemon *Daemon) startSeederDownload(ctx context.Context, r *types.StartDownloadRequest) (*types.StartDownloadResponse, error) {
	fw, err := os.OpenFile(r.Stdout, syscall.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		fw.Close()
	}()

	writeReport := func(f string, a ...interface{}) {
		fmt.Fprintf(fw, f, a...)
	}

	imageSource := r.Source
	if imageSource == "" {
		return nil, fmt.Errorf("Image source can't be nil")
	}

	policyContext, err := daemon.getPolicyContext()
	if err != nil {
		return nil, fmt.Errorf("Error loading trust policy: %v", err)
	}
	defer policyContext.Destroy()

	srcRef, err := transports.ParseImageName(imageSource)
	if err != nil {
		return nil, fmt.Errorf("Invalid source name %s: %v", imageSource, err)
	}

	writeReport("Inspect %s\n", imageSource)
	img, err := srcRef.NewImage(daemon.getSystemContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("Error new image %v", err)
	}

	imgInfo, err := img.Inspect()
	if err != nil {
		return nil, err
	}
	log.Debugf("ImageInfo: %#v", imgInfo)

	imageDest := daemon.buildOciDestFromReference(srcRef)
	log.Debugf("Image destination: %s", imageDest)
	destRef, err := transports.ParseImageName(imageDest)
	if err != nil {
		return nil, fmt.Errorf("Invalid destination name %s: %v", imageDest, err)
	}

	err = copy.Image(daemon.getSystemContext(ctx), policyContext, destRef, srcRef, daemon.getCopyOptions(fw))
	if err != nil {
		return nil, err
	}

	for _, layer := range imgInfo.Layers {
		log.Debugf("Start seeding layer %s", layer)
		err = daemon.startSeedingLayer(srcRef, layer, writeReport)
		if err != nil {
			return nil, err
		}
	}

	return &types.StartDownloadResponse{}, nil
}

func (daemon *Daemon) startSeedingLayer(ref imagetypes.ImageReference, digest string, writeReport func(f string, a ...interface{})) error {
	id := distdigests.Digest(digest).Hex()
	// Write layer to file
	fn := daemon.btEngine.GetFilePath(id)
	layerFile, err := os.Create(fn)
	if err != nil {
		return fmt.Errorf("Create layer file %s failed: %v", fn, err)
	}
	defer layerFile.Close()

	// Copy from OCI directory
	srcPath, err := daemon.buildOciLayerPathFromReference(ref, digest)
	if err != nil {
		log.Errorf("Build oci layer directory error: %v", err)
		return err
	}

	srcFile, err := os.Open(srcPath)
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

func (daemon *Daemon) startLeecherDownload(ctx context.Context, r *types.StartDownloadRequest) (*types.StartDownloadResponse, error) {
	sysCtx := daemon.getSystemContext(ctx)

	fw, err := os.OpenFile(r.Stdout, syscall.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		fw.Close()
	}()

	writeReport := func(f string, a ...interface{}) {
		fmt.Fprintf(fw, f, a...)
	}

	imageSource := r.Source
	if imageSource == "" {
		return nil, fmt.Errorf("Image source cannot be empty")
	}

	policyContext, err := daemon.getPolicyContext()
	if err != nil {
		return nil, fmt.Errorf("Error loading trust policy: %v", err)
	}
	defer policyContext.Destroy()

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

	writeReport("Start download image: %s\n", imageSource)
	for _, layer := range layerInfos {
		err = daemon.startLeechingLayer(srcRef, layer, writeReport, fw)
		if err != nil {
			return nil, err
		}
	}

	imageDest := daemon.buildOciDestFromReference(srcRef)
	log.Debugf("Image destination: %s", imageDest)
	destRef, err := transports.ParseImageName(imageDest)
	if err != nil {
		return nil, fmt.Errorf("Invalid destination name %s: %v", imageDest, err)
	}
	dest, err := destRef.NewImageDestination(sysCtx)
	if err != nil {
		return nil, fmt.Errorf("Error initializing destination %s: %v", imageDest, err)
	}

	// Pull image config
	srcInfo := img.ConfigInfo()
	if srcInfo.Digest != "" {
		writeReport("Copying config %s\n", srcInfo.Digest)
		configBlob, err := img.ConfigBlob()
		if err != nil {
			return nil, err
		}

		destInfo, err := dest.PutBlob(bytes.NewReader(configBlob), srcInfo)
		if err != nil {
			return nil, err
		}

		if destInfo.Digest != srcInfo.Digest {
			return nil, fmt.Errorf("Error config blob %s changed to %s", srcInfo.Digest, destInfo.Digest)
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
	if err = dest.PutManifest(manifest); err != nil {
		return nil, fmt.Errorf("Error writing manifest: %v", err)
	}

	return &types.StartDownloadResponse{}, nil
}

func (daemon *Daemon) startLeechingLayer(ref imagetypes.ImageReference, layer imagetypes.BlobInfo, writeReport func(f string, a ...interface{}), fw io.Writer) error {
	id := distdigests.Digest(layer.Digest).Hex()

	log.Debugf("Start leeching layer %s", id)
	writeReport("%s: Get torrent data from seeder\n", id)
	t, err := daemon.getTorrentFromSeeder(id)
	if err != nil {
		log.Errorf("Get torrent data from seeder for %s failed: %v", id, err)
		return err
	}

	progress := bt.NewProgressDownload(id, int(layer.Size), fw)
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

	dstPath, err := daemon.buildOciLayerPathFromReference(ref, layer.Digest)
	if err != nil {
		return err
	}
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("Create OCI layer %s error: %v", layer.Digest, err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, layerFile)
	if err != nil {
		return fmt.Errorf("Copy OCI layer %s error: %v", layer.Digest, err)
	}
	return nil
}

func (daemon *Daemon) StopDownload(ctx context.Context, r *types.StopDownloadRequest) (*types.StopDownloadResponse, error) {
	imageSource := r.Source
	if imageSource == "" {
		return nil, fmt.Errorf("Image source cannot be empty")
	}

	ociSource, err := daemon.buildOciImageSource(imageSource)
	if err != nil {
		log.Errorf("Build oci image source error: %v", err)
		return nil, err
	}

	log.Debugf("Stop oci image %s", ociSource)
	layers, err := daemon.getImageLayers(ctx, ociSource)
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, layer := range layers {
		id := distdigests.Digest(layer).Hex()
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

	ociSource, err := daemon.buildOciImageSource(imageSource)
	if err != nil {
		log.Errorf("Build oci image source error: %v", err)
		return nil, err
	}

	log.Debugf("Status oci image %s", ociSource)
	layers, err := daemon.getImageLayers(ctx, ociSource)
	if err != nil {
		return nil, err
	}

	var lss []*types.LayerDownState
	for _, layer := range layers {
		id := distdigests.Digest(layer).Hex()

		s, err := daemon.btEngine.GetStatus(id)
		if err != nil {
			return nil, err
		}

		ls := &types.LayerDownState{
			Id:        id,
			State:     s.State,
			Completed: s.Completed,
			Size:      s.TotalLen,
			Seeding:   s.Seeding,
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

func (daemon *Daemon) getImageLayers(ctx context.Context, image string) ([]string, error) {
	policyContext, err := daemon.getPolicyContext()
	if err != nil {
		return nil, fmt.Errorf("Error loading trust policy: %v", err)
	}
	defer policyContext.Destroy()

	srcRef, err := transports.ParseImageName(image)
	if err != nil {
		return nil, fmt.Errorf("Invalid source name %s: %v", image, err)
	}

	img, err := srcRef.NewImage(daemon.getSystemContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("Error new image %v", err)
	}
	imgInfo, err := img.Inspect()
	if err != nil {
		return nil, err
	}
	log.Debugf("ImageInfo: %v", imgInfo)

	return imgInfo.Layers, nil
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

func (daemon *Daemon) getCopyOptions(w io.Writer) *copy.Options {
	return &copy.Options{
		RemoveSignatures: false,
		SignBy:           "",
		ReportWriter:     w,
	}
}

func (daemon *Daemon) buildOciDestFromReference(ref imagetypes.ImageReference) string {
	rootDir := daemon.ociRootDir()
	repoDir := path.Join(rootDir, ref.DockerReference().RemoteName())
	if err := os.MkdirAll(repoDir, 0700); err != nil && !os.IsExist(err) {
		return ""
	}

	refTag := reference.WithDefaultTag(ref.DockerReference())
	tag := refTag.(reference.NamedTagged).Tag()
	imageDest := "oci:" + repoDir + ":" + tag
	return imageDest
}

func (daemon *Daemon) buildOciImageSource(source string) (string, error) {
	rootDir := daemon.ociRootDir()
	name, err := reference.ParseNamed(source)
	if err != nil {
		return "", err
	}

	repoDir := path.Join(rootDir, name.RemoteName())
	if _, err := os.Stat(repoDir); err != nil {
		return "", err
	}

	nameTag := reference.WithDefaultTag(name)
	tag := nameTag.(reference.NamedTagged).Tag()
	imageSource := "oci:" + repoDir + ":" + tag
	return imageSource, nil
}

func (daemon *Daemon) buildOciLayerPathFromReference(ref imagetypes.ImageReference, digest string) (string, error) {
	pts := strings.SplitN(digest, ":", 2)
	if len(pts) != 2 {
		return "", fmt.Errorf("Invalid digest: %s", digest)
	}

	rootDir := daemon.ociRootDir()
	repoDir := path.Join(rootDir, ref.DockerReference().RemoteName())

	layerDir := path.Join(repoDir, "blobs", pts[0])
	if err := os.MkdirAll(layerDir, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}

	dest := path.Join(repoDir, "blobs", pts[0], pts[1])
	return dest, nil
}

func (daemon *Daemon) ociRootDir() string {
	return path.Join(daemon.config.Root, "oci")
}

func (daemon *Daemon) btRootDir() string {
	return path.Join(daemon.config.Root, "bt")
}
