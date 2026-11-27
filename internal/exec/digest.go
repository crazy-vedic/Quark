package exec

import (
	"crypto/md5" //nolint:gosec // RFC 7616 Digest auth still defines MD5; servers choose it.
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strings"
)

// digestChallenge is one parsed `WWW-Authenticate: Digest ...` challenge.
type digestChallenge struct {
	realm     string
	nonce     string
	opaque    string
	algorithm string // upper-cased; "MD5" when the server omits it
	qop       []string
	userhash  bool
}

// errDigestUnsupported reports a challenge Quark cannot answer.
var errDigestUnsupported = errors.New("unsupported digest challenge")

// pickDigestChallenge returns the strongest Digest challenge Quark can
// answer from the WWW-Authenticate values of a 401 response. Servers may
// offer several (RFC 7616 §3.7); SHA-256 is preferred over MD5.
func pickDigestChallenge(values []string) (digestChallenge, bool) {
	var best digestChallenge
	found := false
	for _, value := range values {
		for _, raw := range splitChallenges(value) {
			ch, ok := parseDigestChallenge(raw)
			if !ok || digestHashFunc(ch.algorithm) == nil {
				continue
			}
			if !found || digestStrength(ch.algorithm) > digestStrength(best.algorithm) {
				best, found = ch, true
			}
		}
	}
	return best, found
}

func digestStrength(algorithm string) int {
	if strings.HasPrefix(algorithm, "SHA-256") {
		return 2
	}
	return 1
}

// splitChallenges splits a header value that may hold several challenges
// ("Digest a=1, b=2, Basic realm=x") into one string per scheme. A token
// followed by a space (rather than "=") starts a new challenge.
func splitChallenges(value string) []string {
	var out []string
	start := 0
	inQuote := false
	for i := 0; i < len(value); i++ {
		switch c := value[i]; {
		case c == '"' && (i == 0 || value[i-1] != '\\'):
			inQuote = !inQuote
		case c == ',' && !inQuote:
			rest := strings.TrimLeft(value[i+1:], " \t")
			end := strings.IndexAny(rest, " =")
			if end > 0 && rest[end] == ' ' &&
				!strings.HasPrefix(strings.TrimLeft(rest[end:], " "), "=") {
				out = append(out, value[start:i])
				start = i + 1
			}
		}
	}
	return append(out, value[start:])
}

func parseDigestChallenge(raw string) (digestChallenge, bool) {
	raw = strings.TrimSpace(raw)
	scheme, params, _ := strings.Cut(raw, " ")
	if !strings.EqualFold(scheme, "Digest") {
		return digestChallenge{}, false
	}
	fields := parseAuthParams(params)
	ch := digestChallenge{
		realm:     fields["realm"],
		nonce:     fields["nonce"],
		opaque:    fields["opaque"],
		algorithm: strings.ToUpper(strings.TrimSpace(fields["algorithm"])),
		userhash:  strings.EqualFold(fields["userhash"], "true"),
	}
	if ch.algorithm == "" {
		ch.algorithm = "MD5"
	}
	for _, q := range strings.Split(fields["qop"], ",") {
		if q = strings.ToLower(strings.TrimSpace(q)); q != "" {
			ch.qop = append(ch.qop, q)
		}
	}
	if ch.nonce == "" {
		return digestChallenge{}, false
	}
	return ch, true
}

// parseAuthParams parses comma-separated name=value pairs where values may
// be quoted strings with backslash escapes.
func parseAuthParams(s string) map[string]string {
	out := make(map[string]string)
	for i := 0; i < len(s); {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == ',') {
			i++
		}
		eq := strings.IndexByte(s[i:], '=')
		if eq < 0 {
			break
		}
		name := strings.ToLower(strings.TrimSpace(s[i : i+eq]))
		i += eq + 1
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		var value strings.Builder
		if i < len(s) && s[i] == '"' {
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				value.WriteByte(s[i])
				i++
			}
			i++ // closing quote
		} else {
			for i < len(s) && s[i] != ',' {
				value.WriteByte(s[i])
				i++
			}
		}
		out[name] = strings.TrimSpace(value.String())
	}
	return out
}

func digestHashFunc(algorithm string) func() hash.Hash {
	switch algorithm {
	case "MD5", "MD5-SESS":
		return md5.New
	case "SHA-256", "SHA-256-SESS":
		return sha256.New
	default:
		return nil
	}
}

// digestAuthorization computes the Authorization header value answering ch
// for a request with the given method and request-URI. cnonce is the client
// nonce; callers pass a random value (tests pass a fixed one).
func digestAuthorization(
	ch digestChallenge,
	username, password, method, uri, cnonce string,
) (string, error) {
	newHash := digestHashFunc(ch.algorithm)
	if newHash == nil {
		return "", fmt.Errorf("%w: algorithm %q", errDigestUnsupported, ch.algorithm)
	}
	h := func(s string) string {
		sum := newHash()
		_, _ = io.WriteString(sum, s)
		return hex.EncodeToString(sum.Sum(nil))
	}

	qop := ""
	if len(ch.qop) > 0 {
		for _, q := range ch.qop {
			if q == "auth" {
				qop = "auth"
			}
		}
		if qop == "" {
			return "", fmt.Errorf("%w: qop %q (only \"auth\" is supported)",
				errDigestUnsupported, strings.Join(ch.qop, ","))
		}
	}

	ha1 := h(username + ":" + ch.realm + ":" + password)
	if strings.HasSuffix(ch.algorithm, "-SESS") {
		ha1 = h(ha1 + ":" + ch.nonce + ":" + cnonce)
	}
	ha2 := h(method + ":" + uri)
	const nc = "00000001"
	var response string
	if qop == "" {
		// RFC 2069 compatibility: the server offered no qop.
		response = h(ha1 + ":" + ch.nonce + ":" + ha2)
	} else {
		response = h(ha1 + ":" + ch.nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	}

	user := username
	if ch.userhash {
		user = h(username + ":" + ch.realm)
	}
	parts := []string{
		"username=" + quoteDigest(user),
		"realm=" + quoteDigest(ch.realm),
		"nonce=" + quoteDigest(ch.nonce),
		"uri=" + quoteDigest(uri),
		"algorithm=" + ch.algorithm,
		"response=" + quoteDigest(response),
	}
	if ch.opaque != "" {
		parts = append(parts, "opaque="+quoteDigest(ch.opaque))
	}
	if qop != "" {
		parts = append(parts, "qop="+qop, "nc="+nc, "cnonce="+quoteDigest(cnonce))
	}
	if ch.userhash {
		parts = append(parts, "userhash=true")
	}
	return "Digest " + strings.Join(parts, ", "), nil
}

func quoteDigest(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func newDigestCnonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// maxDigestDrain bounds how much of the 401 body is read before the
// connection is reused for the authenticated retry.
const maxDigestDrain = 64 << 10

// retryWithDigest answers a 401 Digest challenge by resending httpReq once
// with an Authorization header. A 401 without a Digest challenge is returned
// unchanged, so the caller shows it as the response.
func (e *Executor) retryWithDigest(
	httpReq *http.Request,
	resp *http.Response,
	username, password string,
) (*http.Response, error) {
	ch, ok := pickDigestChallenge(resp.Header.Values("WWW-Authenticate"))
	if !ok {
		return resp, nil
	}
	cnonce, err := newDigestCnonce()
	if err != nil {
		return resp, fmt.Errorf("exec: digest auth: %w", err)
	}
	authorization, err := digestAuthorization(
		ch, username, password, httpReq.Method, httpReq.URL.RequestURI(), cnonce,
	)
	if err != nil {
		// The 401 is still a valid response; show it rather than fail.
		e.logger.Warn("exec: digest auth: cannot answer challenge", "err", err)
		return resp, nil
	}

	retry := httpReq.Clone(httpReq.Context())
	if httpReq.GetBody != nil {
		body, err := httpReq.GetBody()
		if err != nil {
			return resp, fmt.Errorf("exec: digest auth: rewind body: %w", err)
		}
		retry.Body = body
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDigestDrain))
	_ = resp.Body.Close()

	retry.Header.Set("Authorization", authorization)
	return e.client.Do(retry)
}
