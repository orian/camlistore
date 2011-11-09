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

package index

import (
	"os"
	"time"

	"camli/blobref"
	"camli/blobserver"
	"camli/search"
)

type IndexStorage interface {
	Set(key, value string) os.Error
	Delete(key string) os.Error

	BeginBatch() BatchMutation
	CommitBatch(b BatchMutation) os.Error
}

type BatchMutation interface {
	Set(key, value string)
	Delete(key string)
}

type mutation struct {
	key    string
	value  string // used if !delete
	delete bool   // if to be deleted
}

type batch struct {
	m []mutation
}

func (b *batch) Delete(key string) {
	b.m = append(b.m, mutation{key: key, delete: true})
}

func (b *batch) Set(key, value string) {
	b.m = append(b.m, mutation{key: key, value: value})
}

type Index struct {
	*blobserver.SimpleBlobHubPartitionMap
	*blobserver.NoImplStorage

	s IndexStorage

	KeyFetcher blobref.StreamingFetcher // for verifying claims

	// Used for fetching blobs to find the complete sha1s of file & bytes
	// schema blobs.
	BlobSource blobserver.Storage
}

var _ blobserver.Storage = (*Index)(nil)
var _ search.Index = (*Index)(nil)

func New(s IndexStorage) *Index {
	return &Index{
		s: s,
		SimpleBlobHubPartitionMap: &blobserver.SimpleBlobHubPartitionMap{},
	}
}

func (x *Index) GetRecentPermanodes(dest chan *search.Result,
	owner []*blobref.BlobRef,
	limit int) os.Error {
	defer close(dest)
	// TODO(bradfitz): this will need to be a context wrapper too, like storage
	return os.NewError("TODO: GetRecentPermanodes")
}

func (x *Index) SearchPermanodesWithAttr(dest chan<- *blobref.BlobRef,
	request *search.PermanodeByAttrRequest) os.Error {
	return os.NewError("TODO: SearchPermanodesWithAttr")
}

func (x *Index) GetOwnerClaims(permaNode, owner *blobref.BlobRef) (search.ClaimList, os.Error) {
	return nil, os.NewError("TODO: GetOwnerClaims")
}

func (x *Index) GetBlobMimeType(blob *blobref.BlobRef) (mime string, size int64, err os.Error) {
	err = os.NewError("TODO: GetBlobMimeType")
	return
}

func (x *Index) ExistingFileSchemas(bytesRef *blobref.BlobRef) ([]*blobref.BlobRef, os.Error) {
	return nil, os.NewError("TODO: xxx")
}

func (x *Index) GetFileInfo(fileRef *blobref.BlobRef) (*search.FileInfo, os.Error) {
	return nil, os.NewError("TODO: GetFileInfo")
}

func (x *Index) PermanodeOfSignerAttrValue(signer *blobref.BlobRef, attr, val string) (*blobref.BlobRef, os.Error) {
	return nil, os.NewError("TODO: PermanodeOfSignerAttrValue")
}

func (x *Index) PathsOfSignerTarget(signer, target *blobref.BlobRef) ([]*search.Path, os.Error) {
	return nil, os.NewError("TODO: PathsOfSignerTarget")
}

func (x *Index) PathsLookup(signer, base *blobref.BlobRef, suffix string) ([]*search.Path, os.Error) {
	return nil, os.NewError("TODO: PathsLookup")
}

func (x *Index) PathLookup(signer, base *blobref.BlobRef, suffix string, at *time.Time) (*search.Path, os.Error) {
	return nil, os.NewError("TODO: PathLookup")
}