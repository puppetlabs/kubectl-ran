package ran

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/client-go/tools/remotecommand"
)

func (o *Options) copyToPod(ctx context.Context, src, dst, pod string) error {
	if _, err := os.Stat(src); err != nil {
		o.Info("skipping copy of %q to pod: %v", src, err)
		return nil
	}

	cmdArr := []string{"tar", "-xmf", "-"}
	if strings.HasPrefix(dst, "/") {
		cmdArr = append(cmdArr, "-C", "/")
		dst = dst[1:]
	}

	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		if err := o.makeTar(src, dst, writer); err != nil {
			o.Warn("unable to tar local files %q: %v", src, err)
		}
	}()

	return o.executor.execute(pod, o.namespace, cmdArr,
		remotecommand.StreamOptions{Stdin: reader, Stdout: o.Out, Stderr: o.ErrOut})
}

func (o *Options) copyFromPod(ctx context.Context, src, dst, pod string) error {
	cmdArr := []string{"tar", "cf", "-"}
	src = strings.TrimPrefix(src, "/")

	reader, outStream := io.Pipe()
	go func() {
		defer outStream.Close()
		if err := o.executor.execute(pod, o.namespace, append(cmdArr, src),
			remotecommand.StreamOptions{Stdout: outStream, Stderr: o.ErrOut}); err != nil {
			o.Warn("unable to tar pod files %q: %v", src, err)
		}
	}()

	return o.untarAll(src, dst, reader)
}

func (o *Options) makeTar(src, dst string, writer io.Writer) error {
	src, dst = filepath.Clean(src), filepath.Clean(dst)
	o.Info("copy from local %q to remote %v", src, dst)
	tw := tar.NewWriter(writer)
	defer tw.Close()

	err := filepath.Walk(src, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			o.Warn("unable to walk directory %q: %v", file, err)
		}

		header, err := tar.FileInfoHeader(fi, file)
		if err != nil {
			return err
		}

		// rewrite for destination path
		header.Name = dst + strings.TrimPrefix(file, src)
		o.Info("%q -> %q", file, header.Name)

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
		o.Warn("failures while adding to tar: %v", err)
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("unable to close tar: %w", err)
	}
	return nil
}

func (o *Options) untarAll(src, dst string, reader io.Reader) error {
	src, dst = filepath.Clean(src), filepath.Clean(dst)
	o.Info("copy from remote %q to local %q", src, dst)
	tr := tar.NewReader(reader)

	for {
		header, err := tr.Next()
		if err != nil {
			if err != io.EOF {
				return err
			}
			break
		}

		if filepath.IsAbs(header.Name) {
			return fmt.Errorf("unexpected tar format, leading '/' was not removed for %q", header.Name)
		}

		// rewrite for destination path
		file := dst + strings.TrimPrefix(header.Name, src)
		o.Info("%q -> %q", header.Name, file)

		if header.FileInfo().IsDir() {
			if err := os.MkdirAll(file, 0755); err != nil {
				return fmt.Errorf("error untaring directory %q: %w", file, err)
			}
			continue
		}

		if header.FileInfo().Mode()&os.ModeSymlink != 0 {
			o.Warn("skipping symlink: %q -> %q", file, header.Linkname)
			continue
		}

		outFile, err := os.Create(file)
		if err != nil {
			return fmt.Errorf("unable to create local file %q: %w", file, err)
		}
		// Ensure closed in case of other errors.
		defer outFile.Close()

		if _, err := io.Copy(outFile, tr); err != nil {
			return fmt.Errorf("error untaring file %q: %w", file, err)
		}
		if err := outFile.Close(); err != nil {
			return fmt.Errorf("error closing file %q: %w", file, err)
		}
	}

	return nil
}
