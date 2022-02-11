package ran

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"k8s.io/client-go/tools/remotecommand"
)

func (o *Options) copyToPod(ctx context.Context, src, dst, pod string) error {
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("%s doesn't exist in local filesystem", src)
	}

	reader, writer := io.Pipe()
	go func(src, dst string, writer io.WriteCloser) {
		defer writer.Close()
		if err := makeTar(src, dst, writer); err != nil {
			fmt.Println("unable to tar source", src, err)
		}
	}(src, dst, writer)

	cmdArr := []string{"tar", "-xmf", "-"}

	destDir := filepath.Dir(dst)
	if len(destDir) > 0 {
		cmdArr = append(cmdArr, "-C", destDir)
	}

	return o.executor.execute(pod, o.namespace, cmdArr,
		remotecommand.StreamOptions{Stdin: reader, Stdout: o.Out, Stderr: o.ErrOut})
}

func makeTar(src, dst string, writer io.Writer) error {
	tw := tar.NewWriter(writer)
	defer tw.Close()

	err := filepath.Walk(src, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			fmt.Println("unable to walk directory", file, err)
		}

		header, err := tar.FileInfoHeader(fi, file)
		if err != nil {
			return err
		}

		// TODO: rewrite for destination path
		header.Name = filepath.ToSlash(file)

		// write header
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		// if not a dir, write file content
		if !fi.IsDir() {
			data, err := os.Open(file)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, data); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		fmt.Println("failures while adding to tar:", err)
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("unable to close tar: %w", err)
	}
	return nil
}
