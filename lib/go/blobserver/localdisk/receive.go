/*
Copyright 2011 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package localdisk

import (
	"camli/blobref"
	"camli/blobserver"
	"exec"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"os"
)

var flagOpenImages = flag.Bool("showimages", false, "Show images on receiving them with eog.")

var CorruptBlobError = os.NewError("corrupt blob; digest doesn't match")

func (ds *diskStorage) ReceiveBlob(blobRef *blobref.BlobRef, source io.Reader, mirrorPartitions []blobserver.Partition) (blobGot *blobref.SizedBlobRef, err os.Error) {
	hashedDirectory := ds.blobDirectoryName(blobRef)
	err = os.MkdirAll(hashedDirectory, 0700)
	if err != nil {
		return
	}

	var tempFile *os.File
	tempFile, err = ioutil.TempFile(hashedDirectory, BlobFileBaseName(blobRef)+".tmp")
	if err != nil {
		return
	}

	success := false // set true later
	defer func() {
		if !success {
			log.Println("Removing temp file: ", tempFile.Name())
			os.Remove(tempFile.Name())
		}
	}()

	hash := blobRef.Hash()
	var written int64
	written, err = io.Copy(io.MultiWriter(hash, tempFile), source)
	if err != nil {
		return
	}
	if err = tempFile.Sync(); err != nil {
		return
	}
	if err = tempFile.Close(); err != nil {
		return
	}

	if !blobRef.HashMatches(hash) {
		err = CorruptBlobError
		return
	}

	fileName := ds.blobFileName(blobRef)
	if err = os.Rename(tempFile.Name(), fileName); err != nil {
		return
	}

	stat, err := os.Lstat(fileName)
	if err != nil {
		return
	}
	if !stat.IsRegular() || stat.Size != written {
		err = os.NewError("Written size didn't match.")
		return
	}

	for _, partition := range mirrorPartitions {
		partitionDir := ds.blobPartitionDirectoryName(partition, blobRef)
		if err = os.MkdirAll(partitionDir, 0700); err != nil {
			return
		}
		partitionFileName := ds.partitionBlobFileName(partition, blobRef)
		if err = os.Link(fileName, partitionFileName); err != nil {
			return
		}
		log.Printf("Mirrored to partition %q", partition)
	}

	blobGot = &blobref.SizedBlobRef{BlobRef: blobRef, Size: stat.Size}
	success = true

	if *flagOpenImages {
		exec.Run("/usr/bin/eog",
			[]string{"/usr/bin/eog", fileName},
			os.Environ(),
			"/",
			exec.DevNull,
			exec.DevNull,
			exec.MergeWithStdout)
	}

	hub := ds.GetBlobHub(blobserver.DefaultPartition)
	hub.NotifyBlobReceived(blobRef)
	for _, partition := range mirrorPartitions {
		hub = ds.GetBlobHub(partition)
		hub.NotifyBlobReceived(blobRef)
	}

	return
}