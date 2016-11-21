package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/containers/image/docker/reference"
	"github.com/containers/image/manifest"
	imagetypes "github.com/containers/image/types"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/net/context"

	"github.com/hustcat/oci-torrent/oci"
)

type blobInfo struct {
	digest string
	size   int64
}

type OciImage struct {
	path   string // directory
	ref    string // reference name
	layout oci.Layout
}

func (i *OciImage) Close() {
	i.layout.Close()
}

func newOciImage(daemon *Daemon, srcRef imagetypes.ImageReference) (*OciImage, error) {
	repoDir, refTag := daemon.buildOciDestFromReference(srcRef)
	layout, err := oci.Open(repoDir)
	if err != nil {
		return nil, err
	}
	return &OciImage{
		path:   repoDir,
		ref:    refTag,
		layout: layout,
	}, nil
}

func newOciImageSimple(daemon *Daemon, ref reference.Named) (*OciImage, error) {
	repoDir, refTag := daemon.buildOciDestSimple(ref)
	layout, err := oci.Open(repoDir)
	if err != nil {
		return nil, err
	}
	return &OciImage{
		path:   repoDir,
		ref:    refTag,
		layout: layout,
	}, nil
}

func ociSupportedManifestMIMETypes() []string {
	return []string{
		imgspecv1.MediaTypeImageManifest,
		manifest.DockerV2Schema2MediaType,
	}
}

func (daemon *Daemon) ociRootDir() string {
	return path.Join(daemon.config.Root, "oci")
}

func (daemon *Daemon) buildOciDestFromReference(ref imagetypes.ImageReference) (string, string) {
	rootDir := daemon.ociRootDir()
	repoDir := path.Join(rootDir, ref.DockerReference().RemoteName())

	refTag := reference.WithDefaultTag(ref.DockerReference())
	tag := refTag.(reference.NamedTagged).Tag()

	return repoDir, tag
}

// No transport prefix
func (daemon *Daemon) buildOciDestSimple(refTag reference.Named) (string, string) {
	rootDir := daemon.ociRootDir()
	repoDir := path.Join(rootDir, refTag.RemoteName())

	tag := refTag.(reference.NamedTagged).Tag()

	return repoDir, tag
}

// Docker manifest to OCI manifest
func createOciManifest(m []byte) ([]byte, string, error) {
	om := imgspecv1.Manifest{}
	mt := manifest.GuessMIMEType(m)
	switch mt {
	case manifest.DockerV2Schema1MediaType, manifest.DockerV2Schema1SignedMediaType:
		// There a simple reason about not yet implementing this.
		// OCI image-spec assure about backward compatibility with docker v2s2 but not v2s1
		// generating a v2s2 is a migration docker does when upgrading to 1.10.3
		// and I don't think we should bother about this now (I don't want to have migration code here in skopeo)
		return nil, "", errors.New("can't create an OCI manifest from Docker V2 schema 1 manifest")
	case manifest.DockerV2Schema2MediaType:
		if err := json.Unmarshal(m, &om); err != nil {
			return nil, "", err
		}
		om.MediaType = imgspecv1.MediaTypeImageManifest
		for i := range om.Layers {
			om.Layers[i].MediaType = imgspecv1.MediaTypeImageLayer
		}
		om.Config.MediaType = imgspecv1.MediaTypeImageConfig
		b, err := json.Marshal(om)
		if err != nil {
			return nil, "", err
		}
		return b, om.MediaType, nil
	case manifest.DockerV2ListMediaType:
		return nil, "", errors.New("can't create an OCI manifest from Docker V2 schema 2 manifest list")
	case imgspecv1.MediaTypeImageManifestList:
		return nil, "", errors.New("can't create an OCI manifest from OCI manifest list")
	case imgspecv1.MediaTypeImageManifest:
		return m, mt, nil
	}
	return nil, "", fmt.Errorf("unrecognized manifest media type %q", mt)
}

func (daemon *Daemon) putManifest(ctx context.Context, ociImg *OciImage, raw []byte) error {
	// raw -> OCI manifest
	ociMan, mt, err := createOciManifest(raw)
	if err != nil {
		return err
	}

	// Write manifest
	digest, err := manifest.Digest(ociMan)
	if err != nil {
		return err
	}
	d, _, err := ociImg.layout.PutBlob(ctx, bytes.NewReader(ociMan))
	if err != nil {
		return err
	}
	if d != digest {
		return fmt.Errorf("Error mismatch digest, exp: %s, act: %s", digest, d)
	}

	// Write reference
	desc := &imgspecv1.Descriptor{}
	desc.Digest = digest
	// TODO: beaware and add support for OCI manifest list
	desc.MediaType = mt
	desc.Size = int64(len(ociMan))
	if err = ociImg.layout.PutReference(ctx, ociImg.ref, desc); err != nil {
		return err
	}

	return nil
}

func (daemon *Daemon) getOciImageLayers(ctx context.Context, ociImg *OciImage) ([]blobInfo, error) {
	desc, err := ociImg.layout.GetReference(ctx, ociImg.ref)
	if err != nil {
		return nil, err
	}

	r, err := ociImg.layout.GetBlob(ctx, desc.Digest)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	data := bytes.Buffer{}
	w := bufio.NewWriter(&data)
	_, err = io.Copy(w, r)
	if err != nil {
		return nil, err
	}

	om := imgspecv1.Manifest{}
	err = json.Unmarshal(data.Bytes(), &om)
	if err != nil {
		return nil, err
	}

	layers := []blobInfo{}
	for _, l := range om.Layers {
		i := blobInfo{
			digest: l.Digest,
			size:   l.Size,
		}
		layers = append(layers, i)
	}
	return layers, nil
}
