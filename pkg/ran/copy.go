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
	go func() {
		defer writer.Close()
		if err := makeTar(src, dst, writer); err != nil {
			fmt.Println("unable to tar local files", src, err)
		}
	}()

	cmdArr := []string{"tar", "-xmf", "-"}

	destDir := filepath.Dir(dst)
	if len(destDir) > 0 {
		cmdArr = append(cmdArr, "-C", destDir)
	}

	return o.executor.execute(pod, o.namespace, cmdArr,
		remotecommand.StreamOptions{Stdin: reader, Stdout: o.Out, Stderr: o.ErrOut})
}

func (o *Options) copyFromPod(ctx context.Context, src, dst, pod string) error {
	reader, outStream := io.Pipe()
	go func() {
		defer outStream.Close()
		if err := o.executor.execute(pod, o.namespace, []string{"tar", "cf", "-", src},
			remotecommand.StreamOptions{Stdout: outStream, Stderr: o.ErrOut}); err != nil {
			fmt.Println("unable to tar pod files", src, err)
		}
	}()

	return untarAll(src, dst, reader)
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

func untarAll(src, dst string, reader io.Reader) error {
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
			return fmt.Errorf("unexpected tar format, leading '/' was not removed for %v", header.Name)
		}

		// TODO: rewrite for destination path
		if header.FileInfo().IsDir() {
			if err := os.MkdirAll(header.Name, 0755); err != nil {
				return fmt.Errorf("error untaring directory %v: %w", header.Name, err)
			}
			continue
		}

		if header.FileInfo().Mode()&os.ModeSymlink != 0 {
			fmt.Printf("skipping symlink: %q -> %q\n", header.Name, header.Linkname)
			continue
		}

		outFile, err := os.Create(header.Name)
		if err != nil {
			return fmt.Errorf("unable to create local file %v: %w", header.Name, err)
		}
		// Ensure closed in case of other errors.
		defer outFile.Close()

		if _, err := io.Copy(outFile, tr); err != nil {
			return fmt.Errorf("error untaring file %v: %w", header.Name, err)
		}
		if err := outFile.Close(); err != nil {
			return fmt.Errorf("error closing file %v: %w", header.Name, err)
		}
	}

	return nil
}
