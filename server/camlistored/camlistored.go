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

package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/big"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"camlistore.org/pkg/jsonsign"
	"camlistore.org/pkg/osutil"
	"camlistore.org/pkg/serverconfig"
	"camlistore.org/pkg/webserver"

	// Storage options:
	_ "camlistore.org/pkg/blobserver/cond"
	_ "camlistore.org/pkg/blobserver/localdisk"
	_ "camlistore.org/pkg/blobserver/remote"
	_ "camlistore.org/pkg/blobserver/replica"
	_ "camlistore.org/pkg/blobserver/s3"
	_ "camlistore.org/pkg/blobserver/shard"
	// Indexers: (also present themselves as storage targets)
	_ "camlistore.org/pkg/index" // base indexer + in-memory dev index
	_ "camlistore.org/pkg/index/mongo"
	_ "camlistore.org/pkg/index/mysql"
	_ "camlistore.org/pkg/index/postgres"

	// Handlers:
	_ "camlistore.org/pkg/search"
	_ "camlistore.org/pkg/server" // UI, publish, etc

	"camlistore.org/third_party/code.google.com/p/go.crypto/openpgp"
)

const (
	defCert = "config/selfgen_cert.pem"
	defKey  = "config/selfgen_key.pem"
)

var (
	flagConfigFile = flag.String("configfile", "",
		"Config file to use, relative to the Camlistore configuration directory root. If blank, the default is used or auto-generated.")
	listenFlag = flag.String("listen", "", "host:port to listen on, or :0 to auto-select. If blank, the value in the config will be used instead.")
)

func exitf(pattern string, args ...interface{}) {
	if !strings.HasSuffix(pattern, "\n") {
		pattern = pattern + "\n"
	}
	fmt.Fprintf(os.Stderr, pattern, args...)
	os.Exit(1)
}

// Mostly copied from $GOROOT/src/pkg/crypto/tls/generate_cert.go
func genSelfTLS(listen string) error {
	priv, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %s", err)
	}

	now := time.Now()

	hostname, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("splitting listen failed: %q", err)
	}

	template := x509.Certificate{
		SerialNumber: new(big.Int).SetInt64(0),
		Subject: pkix.Name{
			CommonName:   hostname,
			Organization: []string{hostname},
		},
		NotBefore:    now.Add(-5 * time.Minute).UTC(),
		NotAfter:     now.AddDate(1, 0, 0).UTC(),
		SubjectKeyId: []byte{1, 2, 3, 4},
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("Failed to create certificate: %s", err)
	}

	certOut, err := os.Create(defCert)
	if err != nil {
		return fmt.Errorf("failed to open %s for writing: %s", defCert, err)
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	certOut.Close()
	log.Printf("written %s\n", defCert)

	keyOut, err := os.OpenFile(defKey, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open %s for writing:", defKey, err)
	}
	pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	keyOut.Close()
	log.Printf("written %s\n", defKey)
	return nil
}

// findConfigFile returns the absolute path of the user's
// config file.
// The provided file may be absolute or relative
// to the user's configuration directory.
// If file is empty, a default high-level config is written
// for the user.
func findConfigFile(file string) (absPath string, err error) {
	switch {
	case file == "":
		absPath = osutil.UserServerConfigPath()
		_, err = os.Stat(absPath)
		if os.IsNotExist(err) {
			err = os.MkdirAll(osutil.CamliConfigDir(), 0700)
			if err != nil {
				return
			}
			log.Printf("Generating template config file %s", absPath)
			err = newDefaultConfigFile(absPath)
		}
		return
	case filepath.IsAbs(file):
		absPath = file
	default:
		absPath = filepath.Join(osutil.CamliConfigDir(), file)
	}
	_, err = os.Stat(absPath)
	return
}

func keyIdFromRing(filename string) (keyId string, err error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("reading identity secret ring file: %v", err)
	}
	defer f.Close()
	el, err := openpgp.ReadKeyRing(f)
	if err != nil {
		return "", fmt.Errorf("reading identity secret ring file %s: %v", filename, err)
	}
	if len(el) != 1 {
		return "", fmt.Errorf("identity secret ring file contained %d identities; expected 1", len(el))
	}
	ent := el[0]
	return ent.PrimaryKey.KeyIdShortString(), nil
}

func generateNewSecRing(filename string) (keyId string, err error) {
	ent, err := jsonsign.NewEntity()
	if err != nil {
		return "", fmt.Errorf("generating new identity: %v", err)
	}
	f, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	err = jsonsign.WriteKeyRing(f, openpgp.EntityList([]*openpgp.Entity{ent}))
	if err != nil {
		return "", fmt.Errorf("writing new key ring to %s: %v", filename, err)
	}
	return ent.PrimaryKey.KeyIdShortString(), nil
}

type defaultConfigFile struct {
	Listen             string        `json:"listen"`
	HTTPS              bool          `json:"https"`
	Auth               string        `json:"auth"`
	Identity           string        `json:"identity"`
	IdentitySecretRing string        `json:"identitySecretRing"`
	BlobPath           string        `json:"blobPath"`
	MySQL              string        `json:"mysql"`
	Mongo              string        `json:"mongo"`
	S3                 string        `json:"s3"`
	ReplicateTo        []interface{} `json:"replicateTo"`
	Publish            struct{}      `json:"publish"`
}

func newDefaultConfigFile(path string) error {
	conf := defaultConfigFile{
		Listen:      ":3179",
		HTTPS:       false,
		Auth:        "localhost",
		ReplicateTo: make([]interface{}, 0),
	}

	blobDir := osutil.CamliBlobRoot()
	if err := os.MkdirAll(blobDir, 0700); err != nil {
		return fmt.Errorf("Could not create default blobs directory: %v", err)
	}
	conf.BlobPath = blobDir

	var keyId string
	secRing := osutil.IdentitySecretRing()
	_, err := os.Stat(secRing)
	switch {
	case err == nil:
		keyId, err = keyIdFromRing(secRing)
		log.Printf("Re-using identity with keyId %q found in file %s", keyId, secRing)
	case os.IsNotExist(err):
		keyId, err = generateNewSecRing(secRing)
		log.Printf("Generated new identity with keyId %q in file %s", keyId, secRing)
	}
	if err != nil {
		return fmt.Errorf("Secret ring: %v", err)
	}
	conf.Identity = keyId
	conf.IdentitySecretRing = secRing

	confData, err := json.MarshalIndent(conf, "", "    ")
	if err != nil {
		return fmt.Errorf("Could not json encode config file : %v", err)
	}

	if err := ioutil.WriteFile(path, confData, 0600); err != nil {
		return fmt.Errorf("Could not create or write default server config: %v", err)
	}
	return nil
}

func setupTLS(ws *webserver.Server, config *serverconfig.Config, listen string) {
	cert, key := config.OptionalString("TLSCertFile", ""), config.OptionalString("TLSKeyFile", "")
	if !config.OptionalBool("https", true) {
		return
	}
	if (cert != "") != (key != "") {
		exitf("TLSCertFile and TLSKeyFile must both be either present or absent")
	}

	if cert == defCert && key == defKey {
		_, err1 := os.Stat(cert)
		_, err2 := os.Stat(key)
		if err1 != nil || err2 != nil {
			if os.IsNotExist(err1) || os.IsNotExist(err2) {
				if err := genSelfTLS(listen); err != nil {
					exitf("Could not generate self-signed TLS cert: %q", err)
				}
			} else {
				exitf("Could not stat cert or key: %q, %q", err1, err2)
			}
		}
	}
	if cert == "" && key == "" {
		err := genSelfTLS(listen)
		if err != nil {
			exitf("Could not generate self signed creds: %q", err)
		}
		cert = defCert
		key = defKey
	}
	ws.SetTLS(cert, key)
}

func handleSignals() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)
	for {
		sig := <-c
		sysSig, ok := sig.(syscall.Signal)
		if !ok {
			log.Fatal("Not a unix signal")
		}
		switch sysSig {
		case syscall.SIGHUP:
			log.Print("SIGHUP: restarting camli")
			err := osutil.RestartProcess()
			if err != nil {
				log.Fatal("Failed to restart: " + err.Error())
			}
		default:
			log.Fatal("Received another signal, should not happen.")
		}
	}
}

// listenAndBaseURL finds the configured, default, or inferred listen address
// and base URL from the command-line flags and provided config.
func listenAndBaseURL(config *serverconfig.Config) (listen, baseURL string) {
	baseURL = config.OptionalString("baseURL", "")
	listen = *listenFlag
	listenConfig := config.OptionalString("listen", "")
	// command-line takes priority over config
	if listen == "" {
		listen = listenConfig
		if listen == "" {
			exitf("\"listen\" needs to be specified either in the config or on the command line")
		}
	}
	return
}

func main() {
	flag.Parse()

	fileName, err := findConfigFile(*flagConfigFile)
	if err != nil {
		exitf("Error finding config file %q: %v", fileName, err)
	}
	log.Printf("Using config file %s", fileName)
	config, err := serverconfig.Load(fileName)
	if err != nil {
		exitf("Could not load server config: %v", err)
	}

	ws := webserver.New()
	listen, baseURL := listenAndBaseURL(config)

	setupTLS(ws, config, listen)
	err = config.InstallHandlers(ws, baseURL, nil)
	if err != nil {
		exitf("Error parsing config: %v", err)
	}

	err = ws.Listen(listen)
	if err != nil {
		exitf("Listen: %v", err)
	}

	urlOpened := false
	if config.UIPath != "" {
		uiURL := ws.ListenURL() + config.UIPath
		log.Printf("UI available at %s", uiURL)
		if runtime.GOOS == "windows" {
			// Might be double-clicking an icon with no shell window?
			// Just open the URL for them.
			urlOpened = true
			go osutil.OpenURL(uiURL)
		}
	}
	if *flagConfigFile == "" && !urlOpened {
		go func() {
			err := osutil.OpenURL(ws.ListenURL())
			if err != nil {
				log.Printf("Failed to open %s in browser: %v", baseURL, err)
			}
		}()
	}

	go ws.Serve()
	go handleSignals()
	select {}
}
