## oci-torrent - A tool to distribute oci image with bittorent

`oci-torrent` is a tool to distribute [OCI image](https://github.com/opencontainers/image-spec) with bittorent.

## build

```sh
# make static
# ls bin
oci-torrent-ctr  oci-torrentd
```

## Usage

### Seeder

* Start daemon

```sh
# bin/oci-torrentd --debug --bt-seeder=true --listen="tcp://10.10.10.10:20000" --bt-tracker="http://10.10.10.11:6882/announce"
DEBU[0000] Demon config: &daemon.Config{Pidfile:"", Root:"/data/oci-torrentd", ConnTimeout:1000000000, BtEnable:true, BtSeeder:true, BtTrackers:[]string{"http://10.10.10.11:6882/announce"}, BtSeederServer:[]string{}, UploadRateLimit:0, DownloadRateLimit:0} 
DEBU[0000] Start bt engine succss                       
DEBU[0000] containerd: grpc api on 10.10.10.10:20000   
```

* Start download and seeding

```sh
# bin/oci-torrent-ctr --address="tcp://10.10.10.10:20000" start docker://busybox   
Inspect docker://busybox
Getting image source signatures
Copying blob sha256:56bec22e355981d8ba0878c6c2f23b21f422f30ab0aba188b54f1ffeff59c190
 652.49 KB / 652.49 KB [=======================================================]
Copying config sha256:e02e811dd08fd49e7f6032625495118e63f597eb150403d02e3238af1df240ba
 0 B / 1.43 KB [---------------------------------------------------------------]
Writing manifest to image destination
Storing signatures
Start seeding 56bec22e355981d8ba0878c6c2f23b21f422f30ab0aba188b54f1ffeff59c190
Start seeding 56bec22e355981d8ba0878c6c2f23b21f422f30ab0aba188b54f1ffeff59c190 success
```

* Status

```sh
# bin/oci-torrent-ctr --address="tcp://10.10.10.10:20000" status busybox
ID                  STATE               COMPLETED           TOTALLEN            SEEDING
56bec22e3559        Started             668151              668151              true
```

### Leecher

* Start daemon

```sh
# oci-torrentd --debug --seeder-addr="tcp://10.10.10.10:20000" --bt-tracker="http://10.10.10.11:6882/announce"
DEBU[0000] Demon config: &daemon.Config{Pidfile:"", Root:"/data/oci-torrentd", ConnTimeout:1000000000, BtEnable:true, BtSeeder:false, BtTrackers:[]string{"http://10.10.10.11:6882/announce"}, BtSeederServer:[]string{"tcp://10.10.10.10:20000"}, UploadRateLimit:0, DownloadRateLimit:0} 
DEBU[0000] Start bt engine succss                       
DEBU[0000] containerd: grpc api on /run/oci-torrentd/oci-torrentd.sock
```


* Start download

```sh
# oci-torrent-ctr start docker://busybox
Get layer info docker://busybox
Start download image: docker://busybox
56bec22e355981d8ba0878c6c2f23b21f422f30ab0aba188b54f1ffeff59c190: Get torrent data from seeder
56bec22e355981d8ba0878c6c2f23b21f422f30ab0aba188b54f1ffeff59c190: Getting torrent info
56bec22e355981d8ba0878c6c2f23b21f422f30ab0aba188b54f1ffeff59c190: Start bittorent downloading
 262144 / 668151 [============================================>--------------------------------------------------------------------]56bec22e355981d8ba0878c6c2f23b21f422f30ab0aba188b54f1ffeff59c190: Copy to OCI directory
Copying config sha256:e02e811dd08fd49e7f6032625495118e63f597eb150403d02e3238af1df240ba
Writing manifest to image destination
```

Result:

```
# tree /data/oci-torrentd/oci/
/data/oci-torrentd/oci/
`-- library
    `-- busybox
        |-- blobs
        |   `-- sha256
        |       |-- 56bec22e355981d8ba0878c6c2f23b21f422f30ab0aba188b54f1ffeff59c190
        |       |-- d09bddf04324303fe923f8c2761041046fa08fec4e120b02f5900f450398df9b
        |       `-- e02e811dd08fd49e7f6032625495118e63f597eb150403d02e3238af1df240ba
        |-- oci-layout
        `-- refs
            `-- latest
```

* Status

```sh
# oci-torrent-ctr status busybox            
ID                  STATE               COMPLETED           TOTALLEN            SEEDING
56bec22e3559        Started             668151              668151              true
```

* Stop download

```sh
# oci-torrent-ctr stop busybox      
Stopped: 56bec22e355981d8ba0878c6c2f23b21f422f30ab0aba188b54f1ffeff59c190
```