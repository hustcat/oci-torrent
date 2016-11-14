#!/usr/bin/env bash
set -e

cd "$(dirname "$BASH_SOURCE")/.."
rm -rf vendor/
source 'hack/.vendor-helpers.sh'

clone git github.com/containers/image master
clone git gopkg.in/cheggaaa/pb.v1 ad4efe000aa550bb54918c06ebbadc0ff17687b9 https://github.com/cheggaaa/pb
clone git github.com/Sirupsen/logrus v0.10.0
clone git github.com/go-check/check v1
clone git github.com/stretchr/testify v1.1.3
clone git github.com/davecgh/go-spew master
clone git github.com/pmezard/go-difflib master
# docker deps from https://github.com/docker/docker/blob/v1.11.2/hack/vendor.sh
clone git github.com/docker/docker v1.12.1
clone git github.com/docker/engine-api 4eca04ae18f4f93f40196a17b9aa6e11262a7269
clone git github.com/docker/go-connections v0.2.0
clone git github.com/vbatts/tar-split v0.9.11
clone git github.com/gorilla/context 14f550f51a
clone git github.com/gorilla/mux e444e69cbd
clone git github.com/docker/go-units 651fc226e7441360384da338d0fd37f2440ffbe3
clone git golang.org/x/net master https://github.com/golang/net.git
# end docker deps
clone git github.com/docker/distribution 07f32ac1831ed0fc71960b7da5d6bb83cb6881b5
clone git github.com/docker/libtrust master
clone git github.com/opencontainers/runc master
clone git github.com/opencontainers/image-spec 7dc1ee39c59c6949612c6fdf502a4727750cb063
clone git github.com/mtrmac/gpgme master
# openshift/origin' k8s dependencies as of OpenShift v1.1.5
clone git github.com/golang/glog 44145f04b68cf362d9c4df2182967c2275eaefed
clone git k8s.io/kubernetes 4a3f9c5b19c7ff804cbc1bf37a15c044ca5d2353 https://github.com/openshift/kubernetes
clone git github.com/ghodss/yaml 73d445a93680fa1a78ae23a5839bad48f32ba1ee
clone git gopkg.in/yaml.v2 d466437aa4adc35830964cffc5b5f262c63ddcb4
clone git github.com/imdario/mergo 6633656539c1639d9d78127b7d47c622b5d7b6dc

############### add new
clone git github.com/golang/protobuf 1f49d83d9aa00e6ce4fc8258c71cc7786aec968a
clone git google.golang.org/grpc v1.0.1-GA https://github.com/grpc/grpc-go.git

clone git github.com/codegangsta/cli 9fec0fad02befc9209347cc6d620e68e1b45f74d
clone git github.com/coreos/go-systemd 7b2428fec40033549c68f54e26e89e7ca9a9ce31

# torrent
clone git github.com/anacrolix/torrent mempool https://bitbucket.org/hustcat/torrent.git
clone git github.com/anacrolix/missinggo d718583e4697ab9c715c4fe0b19257b2c70d2683 
clone git github.com/anacrolix/sync 812602587b72df6a2a4f6e30536adc75394a374b
clone git github.com/anacrolix/utp 2d2a5d62549da0b2d2a3c23b7823ee6930ca8e07
clone git github.com/bradfitz/iter 454541ec3da2a73fc34fd049b19ee5777bf19345
clone git github.com/dustin/go-humanize fef948f2d241bd1fd0631108ecc2c9553bae60bf
clone git github.com/edsrzf/mmap-go 935e0e8a636ca4ba70b713f3e38a19e1b77739e8
clone git github.com/mattn/go-sqlite3 e118d4451349065b8e7ce0f0af32e033995363f8
clone git github.com/tylertreat/BoomFilters ae0b9585d4b87b2fe0b49ee09270abae7aaad179
clone git github.com/willf/bloom aa3071d9203598166c09553d43e74b588c3d91be
clone git golang.org/x/time master https://github.com/golang/time.git
clone git github.com/RoaringBitmap/roaring c711b7315585d926fd83117decdcdae581fe6776
clone git github.com/google/btree 7d79101e329e5a3adf994758c578dab82b90c017
clone git github.com/ryszard/goskiplist 2dfbae5fcf46374f166f8969cb07e167f1be6273
clone git github.com/willf/bitset 2e6e8094ef4745224150c88c16191c7dceaad16f

clean

mv vendor/src/* vendor/
rm -rf vendor/src
