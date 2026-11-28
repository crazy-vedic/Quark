package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/atomicfile"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/highlight"
)

const maxPrettyJSONBytes = 1 << 20
const maxTerminalBodyBytes = 16 << 20

// runOutput selects where the response goes for quark run.
type runOutput struct {
	file    string // -o: write the body to this file instead of stdout
	include bool   // -i: print the status line and headers on stdout first
}

func printRunResult(
	cmd *cobra.Command,
	req *domain.Request,
	result *exec.ExecuteResult,
	verbose bool,
) error {
	return printRunOutput(cmd, req, result, verbose, runOutput{})
}

// printRunOutput writes the run's diagnostics and response. The status block
// and --verbose details always go to stderr; stdout carries the headers (with
// -i) and the body, unless the body goes to the -o file.
func printRunOutput(
	cmd *cobra.Command,
	req *domain.Request,
	result *exec.ExecuteResult,
	verbose bool,
	opts runOutput,
) (runErr error) {
	defer func() {
		if cleanupErr := result.Cleanup(); cleanupErr != nil {
			cleanupErr = fmt.Errorf("cleanup response body: %w", cleanupErr)
			runErr = errors.Join(runErr, cleanupErr)
		}
	}()
	if verbose {
		printRequestDetails(cmd.ErrOrStderr(), req)
		printResponseDetails(cmd.ErrOrStderr(), result)
	}
	// The status block goes to stderr so stdout carries only the response.
	fmt.Fprintf(cmd.ErrOrStderr(), "Status: %s\nSize:   %d bytes\nTime:   %v\n",
		result.Status, result.Size, result.Duration.Round(time.Millisecond))
	if opts.include {
		printResponseHeaderLines(cmd.OutOrStdout(), result)
	}
	if opts.file != "" {
		return writeRunBodyFile(opts.file, result)
	}
	if result.Size == 0 {
		return nil
	}

	out := cmd.OutOrStdout()
	terminal := false
	if reader, ok := out.(io.Reader); ok {
		terminal = terminalProbe(reader)
	}
	contentType := http.Header(result.Headers).Get("Content-Type")
	if result.TempPath != "" {
		f, err := os.Open(result.TempPath)
		if err != nil {
			return fmt.Errorf("read response body: %w", err)
		}
		defer f.Close()
		if terminal {
			peek := make([]byte, 512)
			n, _ := f.Read(peek)
			peek = peek[:n]
			if binaryResponse(peek, contentType) {
				fmt.Fprintf(
					out,
					"\n[binary response body omitted from terminal: %d bytes]\n",
					result.Size,
				)
				return nil
			}
			_, _ = f.Seek(0, io.SeekStart)
			if result.Size > maxTerminalBodyBytes {
				fmt.Fprintf(
					out,
					"\n[large response body: %d bytes; redirect stdout to capture it]\n",
					result.Size,
				)
				return nil
			}
			body, err := io.ReadAll(f)
			if err != nil {
				return fmt.Errorf("read response body: %w", err)
			}
			return writeResponseBody(out, body, contentType, true)
		}
		if result.Size <= maxPrettyJSONBytes && isJSONResponse(contentType, nil) {
			body, err := io.ReadAll(f)
			if err != nil {
				return fmt.Errorf("read response body: %w", err)
			}
			return writeResponseBody(out, body, contentType, terminal)
		}
		_, err = io.Copy(out, f)
		return err
	}
	return writeResponseBody(out, result.Body, contentType, terminal)
}

// printResponseHeaderLines writes the status and headers for -i, followed by a
// blank line, in the usual HTTP-dump layout.
func printResponseHeaderLines(w io.Writer, result *exec.ExecuteResult) {
	fmt.Fprintln(w, safeText(w, result.Status))
	keys := make([]string, 0, len(result.Headers))
	for key := range result.Headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range result.Headers[key] {
			fmt.Fprintf(w, "%s: %s\n", safeText(w, key), safeText(w, value))
		}
	}
	fmt.Fprintln(w)
}

// writeRunBodyFile saves the response body exactly as received, for -o. The
// file is replaced atomically, like every other file Quark writes.
func writeRunBodyFile(path string, result *exec.ExecuteResult) error {
	err := atomicfile.Write(path, 0o644, func(w io.Writer) error {
		if result.TempPath == "" {
			_, err := w.Write(result.Body)
			return err
		}
		src, err := os.Open(result.TempPath)
		if err != nil {
			return fmt.Errorf("read response body: %w", err)
		}
		defer src.Close()
		_, err = io.Copy(w, src)
		return err
	})
	if err != nil {
		return fmt.Errorf("write output file: %w", err)
	}
	return nil
}

func printRequestDetails(w io.Writer, req *domain.Request) {
	fmt.Fprintf(w, "Request: %s %s\n", safeText(w, req.Method), safeText(w, req.URL))
	if req.Headers != "" {
		fmt.Fprintf(w, "Request headers: %s\n", safeText(w, req.Headers))
	}
	if req.AuthType != "" {
		fmt.Fprintf(w, "Request auth type: %s\n", safeText(w, req.AuthType))
		if req.AuthConfig != "" {
			fmt.Fprintf(w, "Request auth config: %s\n", safeText(w, req.AuthConfig))
		}
	}
	if req.Body != "" {
		fmt.Fprintf(w, "Request body (%d bytes):\n%s\n", len(req.Body), safeText(w, req.Body))
	}
}

func printResponseDetails(w io.Writer, result *exec.ExecuteResult) {
	fmt.Fprintf(w, "Response: %s\n", safeText(w, result.Status))
	keys := make([]string, 0, len(result.Headers))
	for key := range result.Headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range result.Headers[key] {
			fmt.Fprintf(w, "< %s: %s\n", safeText(w, key), safeText(w, value))
		}
	}
	fmt.Fprintln(w)
}

func writeResponseBody(w io.Writer, body []byte, contentType string, terminal bool) error {
	if terminal && binaryResponse(body, contentType) {
		_, err := fmt.Fprintf(
			w,
			"\n[binary response body omitted from terminal: %d bytes]\n",
			len(body),
		)
		return err
	}
	if len(body) == 0 {
		return nil
	}
	if terminal {
		body = []byte(highlight.SafeStrip(string(body)))
	}
	if isJSONResponse(contentType, body) {
		var pretty bytes.Buffer
		if json.Indent(&pretty, body, "", "  ") == nil {
			if terminal {
				_, err := fmt.Fprintln(w, highlight.JSON(pretty.String(), "auto"))
				return err
			}
			body = pretty.Bytes()
		}
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if body[len(body)-1] != '\n' {
		_, err := io.WriteString(w, "\n")
		return err
	}
	return nil
}

func safeText(w io.Writer, value string) string {
	if reader, ok := w.(io.Reader); ok && terminalProbe(reader) {
		return highlight.SafeStrip(value)
	}
	return value
}

func isJSONResponse(contentType string, body []byte) bool {
	return strings.Contains(strings.ToLower(contentType), "json") ||
		(len(body) > 0 && json.Valid(body))
}

func binaryResponse(body []byte, contentType string) bool {
	contentType = strings.ToLower(contentType)
	if strings.HasPrefix(contentType, "text/") ||
		strings.Contains(contentType, "json") ||
		strings.Contains(contentType, "xml") ||
		strings.Contains(contentType, "javascript") ||
		strings.Contains(contentType, "x-www-form-urlencoded") {
		return false
	}
	if strings.HasPrefix(contentType, "application/") && contentType != "application/octet-stream" {
		return true
	}
	if contentType == "application/octet-stream" {
		return true
	}
	if bytes.IndexByte(body, 0) >= 0 || !utf8.Valid(body) {
		return true
	}
	if len(body) >= 4 && !strings.HasPrefix(strings.ToLower(contentType), "application/") {
		return !strings.Contains(http.DetectContentType(body), "text/")
	}
	return false
}
