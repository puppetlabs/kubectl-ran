package ran

import (
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/rest"
	fake "k8s.io/client-go/rest/fake"
	"k8s.io/client-go/tools/remotecommand"
)

type testExecutor struct {
	stdin, stdout, stderr string
}

func (e *testExecutor) Stream(options remotecommand.StreamOptions) (err error) {
	if options.Stdin != nil {
		if bytes, err := io.ReadAll(options.Stdin); err != nil {
			return err
		} else {
			e.stdin = string(bytes)
		}
	}
	if options.Stdout != nil {
		if _, err = io.WriteString(options.Stdout, e.stdout); err != nil {
			return err
		}
	}
	if options.Stderr != nil {
		if _, err = io.WriteString(options.Stderr, e.stderr); err != nil {
			return err
		}
	}
	return nil
}

func TestExecute(t *testing.T) {
	te := &testExecutor{
		stdout: "output",
		stderr: "an error",
	}
	var client fake.RESTClient
	req := assert.New(t)

	ex := newExecutor(nil, &client)
	ex.impl = func(config *rest.Config, method string, url *url.URL) (remotecommand.Executor, error) {
		req.Nil(config)
		req.Equal("POST", method)
		req.Contains(url.String(), "/namespaces/bar/pods/foo/exec?command=echo&command=hello&container=worker&stderr=true&stdin=true&stdout=true")
		return te, nil
	}

	var stdout, stderr strings.Builder
	var execOptions remotecommand.StreamOptions
	execOptions.Stdin = strings.NewReader("some input")
	execOptions.Stdout = &stdout
	execOptions.Stderr = &stderr

	req.NoError(ex.execute("foo", "bar", []string{"echo", "hello"}, execOptions))
	req.Equal(te.stdout, stdout.String())
	req.Equal(te.stderr, stderr.String())
}
