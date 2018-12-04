package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/golang/glog"
	"github.com/gravitational/configure"
	minio "github.com/minio/minio-go"
	"github.com/pborman/uuid"
	"github.com/pkg/errors"
)

type config struct {
	Endpoint        string `env:"APP_UPLOAD_ENDPOINT" cli:"endpoint" yaml:"endpoint"`
	AccessKeyID     string `env:"APP_UPLOAD_ACCESS_KEY" cli:"accesskeyid" yaml:"accesskeyid"`
	SecretAccessKey string `env:"APP_UPLOAD_SECRET_KEYs" cli:"secretaccesskey" yaml:"secretaccesskey"`
	Secure          bool   `env:"SECURE" cli:"secure" yaml:"secure"`
	DataPath        string `env:"DATA_PATH" cli:"data-path" yaml:"data_path"`
	BucketName      string `env:"APP_ASSETS_FOLDER" cli:"bucketname" yaml:"bucketname"`
}

//newConfig constructs a new config with default values
func newConfig() config {
	config := config{}
	config.Endpoint = "127.0.0.1:9000"
	config.Secure = false
	config.AccessKeyID = "D2PL1U22NFIPSR3LIMDP"
	config.SecretAccessKey = "ARoj43ITJX4s0i4s8UqNPR8NIYW+pz6ohcI4u0sQ"
	config.DataPath = "dist/"
	return config
}

func prepareTestDirTree(tree string) (string, error) {
	tmpDir, err := ioutil.TempDir("", "")
	if err != nil {
		return "", fmt.Errorf("Error creating temp directory: %v", err)
	}

	err = os.MkdirAll(filepath.Join(tmpDir, tree), 0755)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	return tmpDir, nil
}

func s3FileName(rootpath string, currentpath string) string {
	return strings.TrimPrefix(currentpath, rootpath+"/")
}

// cancelOnInterrupt calls cancel function when os.Interrupt or SIGTERM is received
func cancelOnInterrupt(ctx context.Context, cancel context.CancelFunc) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-ctx.Done():
		case <-c:
			cancel()
		}
	}()
}

func fatalOnError(err error, context string) {
	if err != nil {
		wrappedError := errors.Wrap(err, context)
		glog.Fatal(wrappedError)
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := NewConfig()
	// parse environment variables
	err := configure.ParseEnv(&cfg)
	// parse command line arguments
	err = configure.ParseCommandLine(&cfg, os.Args[1:])

	if !filepath.IsAbs(cfg.DataPath) {
		cfg.DataPath, _ = filepath.Abs(cfg.DataPath)
	}

	if cfg.BucketName == "" {
		cfg.BucketName = uuid.NewRandom().String()
	}

	fmt.Printf("Starting SPAloyer Job with the following parameters: \n")
	fmt.Printf("   Endpoint:        %s\n", cfg.Endpoint)
	fmt.Printf("   Secure:          %t\n", cfg.Secure)
	fmt.Printf("   AccessKeyID:     %s\n", cfg.AccessKeyID)
	fmt.Printf("   SecretAccessKey: %s\n", cfg.SecretAccessKey)
	fmt.Printf("   DataPath:        %s\n", cfg.DataPath)
	fmt.Printf("   BucketName:      %s\n", cfg.BucketName)

	minioClient, err := minio.New(cfg.Endpoint, cfg.AccessKeyID, cfg.SecretAccessKey, cfg.Secure)
	if err != nil {
		return err
	}
	log.Printf("=> %#v\n", minioClient)
	minioClient.MakeBucket(cfg.BucketName, "us-east-1")

	os.Chdir(cfg.DataPath)
	var fileNumber int
	var uploadSize int64
	err = filepath.Walk(cfg.DataPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("Prevent panic by handling failure accessing a path %q: %v\n", path, err)
			return err
		}
		if !strings.HasSuffix(info.Name(), ".DS_Store") && !info.IsDir() {

			file, err := os.Open(path)
			n, err := minioClient.PutObject(cfg.BucketName, s3FileName(cfg.DataPath, path), file, info.Size(), minio.PutObjectOptions{ContentType: "application/octet-stream"})
			if err != nil {
				fmt.Printf(" - error while uploading '%s'\n", s3FileName(cfg.DataPath, path))
				return err
			}
			fileNumber++
			uploadSize = uploadSize + n
			fmt.Printf(" - succeesfully uploaded '%s'\n", s3FileName(cfg.DataPath, path))
		}
		return nil
	})
	if err != nil {
		fmt.Printf("Error uploading %d files and %d bytes: %v! \n", fileNumber, uploadSize, err)
		return err
	}
	fmt.Printf("Successfully uploaded %d files and %d bytes! \n", fileNumber, uploadSize)
	return nil
}
