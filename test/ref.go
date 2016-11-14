package main

import (
	"fmt"
	"github.com/containers/image/docker/reference"
)

func main() {
	r, err := reference.ParseNamed("docker.io/hustcat/busybox:v1")
	if err != nil {
		fmt.Printf("error: %v\n", err)
	} else {
		fmt.Printf("Name(): %v\n", r.Name())         //hustcat/busybox
		fmt.Printf("Remote(): %v\n", r.RemoteName()) //hustcat/busybox
		fmt.Printf("String(): %v\n", r.String())     //hustcat/busybox:v1
	}
	r, err = reference.ParseNamed("docker.io/busybox:v1")
	if err != nil {
		fmt.Printf("error: %v\n", err)
	} else {
		fmt.Printf("Name(): %v\n", r.Name())         //busybox
		fmt.Printf("Remote(): %v\n", r.RemoteName()) //library/busybox
		fmt.Printf("String(): %v\n", r.String())     //busybox:v1
	}
	r, err = reference.ParseNamed("docker.io/busybox")
	if err != nil {
		fmt.Printf("error: %v\n", err)
	} else {
		r = reference.WithDefaultTag(r)
		fmt.Printf("Name(): %v\n", r.Name())         //busybox
		fmt.Printf("Remote(): %v\n", r.RemoteName()) //library/busybox
		fmt.Printf("String(): %v\n", r.String())     //busybox:v1
		fmt.Printf("Tag(): %v\n", r.(reference.NamedTagged).Tag())
	}

}
