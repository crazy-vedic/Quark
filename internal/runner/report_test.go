package runner

import (
	"bytes"
	"encoding/xml"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleResults() []Result {
	return []Result{
		{
			Name:       "API/login",
			Collection: "API",
			Status:     Pass,
			StatusCode: 200,
			Duration:   120 * time.Millisecond,
		},
		{
			Name: "API/me", Collection: "API", Status: Fail, StatusCode: 500,
			Duration: 30 * time.Millisecond, Messages: []string{"status = 200: status is 500"},
		},
		{
			Name: "API/Users/a # b", Collection: "API/Users", Status: Fail,
			Messages: []string{"request failed: dial tcp: refused", "second \"quoted\" reason"},
		},
	}
}

func TestWriteReportText(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, WriteReport(&out, FormatText, sampleResults()))
	assert.Equal(t, "PASS  API/login  200  120ms\n"+
		"FAIL  API/me  500  30ms\n"+
		"        - status = 200: status is 500\n"+
		"FAIL  API/Users/a # b  -  0s\n"+
		"        - request failed: dial tcp: refused\n"+
		"        - second \"quoted\" reason\n"+
		"\n1 passed, 2 failed\n", out.String())
}

func TestWriteReportJUnitIsWellFormed(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, WriteReport(&out, FormatJUnit, sampleResults()))

	var doc struct {
		XMLName xml.Name `xml:"testsuites"`
		Suites  []struct {
			Name     string `xml:"name,attr"`
			Tests    int    `xml:"tests,attr"`
			Failures int    `xml:"failures,attr"`
			Cases    []struct {
				Name      string `xml:"name,attr"`
				Classname string `xml:"classname,attr"`
				Failure   *struct {
					Message string `xml:"message,attr"`
				} `xml:"failure"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	require.NoError(t, xml.Unmarshal(out.Bytes(), &doc), out.String())
	require.Len(t, doc.Suites, 2)
	assert.Equal(t, "API", doc.Suites[0].Name)
	assert.Equal(t, 2, doc.Suites[0].Tests)
	assert.Equal(t, 1, doc.Suites[0].Failures)
	assert.Equal(t, "API/Users", doc.Suites[1].Name)
	assert.Equal(t, "API/Users", doc.Suites[1].Cases[0].Classname)
	require.NotNil(t, doc.Suites[1].Cases[0].Failure)
	assert.Equal(t, "request failed: dial tcp: refused", doc.Suites[1].Cases[0].Failure.Message)
	assert.Nil(t, doc.Suites[0].Cases[0].Failure)
}

func TestWriteReportTAP(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, WriteReport(&out, FormatTAP, sampleResults()))
	assert.Equal(t, "TAP version 13\n"+
		"1..3\n"+
		"ok 1 - API/login\n"+
		"not ok 2 - API/me\n"+
		"  ---\n"+
		"  message: \"status = 200: status is 500\"\n"+
		"  ...\n"+
		"not ok 3 - API/Users/a \\# b\n"+
		"  ---\n"+
		"  message: \"request failed: dial tcp: refused\"\n"+
		"  messages:\n"+
		"    - \"request failed: dial tcp: refused\"\n"+
		"    - \"second \\\"quoted\\\" reason\"\n"+
		"  ...\n", out.String())
}

func TestWriteReportRejectsUnknownFormat(t *testing.T) {
	var out bytes.Buffer
	assert.Error(t, WriteReport(&out, "yaml", nil))
}
