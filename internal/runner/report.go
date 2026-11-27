package runner

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Formats accepted by WriteReport.
const (
	FormatText  = "text"
	FormatJUnit = "junit"
	FormatTAP   = "tap"
)

// WriteReport writes results in format: "text" (one line per request), "junit"
// (JUnit XML, one testsuite per collection) or "tap" (TAP version 13).
func WriteReport(w io.Writer, format string, results []Result) error {
	switch format {
	case FormatText:
		return writeText(w, results)
	case FormatJUnit:
		return writeJUnit(w, results)
	case FormatTAP:
		return writeTAP(w, results)
	default:
		return fmt.Errorf("unknown report format %q (use text, junit or tap)", format)
	}
}

func writeText(w io.Writer, results []Result) error {
	for _, r := range results {
		if r.Status == Pass {
			fmt.Fprintf(w, "PASS  %s  %s\n", r.Name, textStatus(r))
			continue
		}
		fmt.Fprintf(w, "FAIL  %s  %s\n", r.Name, textStatus(r))
		for _, m := range r.Messages {
			fmt.Fprintf(w, "        - %s\n", m)
		}
	}
	_, err := fmt.Fprintf(w, "\n%s\n", Summary(results))
	return err
}

// textStatus shows the HTTP status and the time, or a dash when the request
// never got a response.
func textStatus(r Result) string {
	code := "-"
	if r.StatusCode != 0 {
		code = strconv.Itoa(r.StatusCode)
	}
	return fmt.Sprintf("%s  %v", code, r.Duration.Round(time.Millisecond))
}

type junitSuites struct {
	XMLName xml.Name     `xml:"testsuites"`
	Suites  []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Time     string      `xml:"time,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitMessage `xml:"failure,omitempty"`
}

type junitMessage struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

func writeJUnit(w io.Writer, results []Result) error {
	var suites []*junitSuite
	byName := map[string]*junitSuite{}
	for _, r := range results {
		s, ok := byName[r.Collection]
		if !ok {
			s = &junitSuite{Name: r.Collection}
			byName[r.Collection] = s
			suites = append(suites, s)
		}
		s.Tests++
		seconds := r.Duration.Seconds()
		c := junitCase{Name: r.Name, Classname: r.Collection, Time: formatSeconds(seconds)}
		if r.Status == Fail {
			s.Failures++
			c.Failure = &junitMessage{
				Message: r.Messages[0],
				Body:    strings.Join(r.Messages, "\n"),
			}
		}
		s.Cases = append(s.Cases, c)
	}
	doc := junitSuites{}
	for _, s := range suites {
		var total time.Duration
		for _, c := range s.Cases {
			if v, err := strconv.ParseFloat(c.Time, 64); err == nil {
				total += time.Duration(v * float64(time.Second))
			}
		}
		s.Time = formatSeconds(total.Seconds())
		doc.Suites = append(doc.Suites, *s)
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

func formatSeconds(s float64) string {
	return strconv.FormatFloat(s, 'f', 3, 64)
}

func writeTAP(w io.Writer, results []Result) error {
	if _, err := fmt.Fprintf(w, "TAP version 13\n1..%d\n", len(results)); err != nil {
		return err
	}
	for i, r := range results {
		name := tapEscape(r.Name)
		if r.Status == Pass {
			fmt.Fprintf(w, "ok %d - %s\n", i+1, name)
			continue
		}
		fmt.Fprintf(w, "not ok %d - %s\n", i+1, name)
		fmt.Fprintf(w, "  ---\n  message: %s\n", strconv.Quote(r.Messages[0]))
		if len(r.Messages) > 1 {
			fmt.Fprintf(w, "  messages:\n")
			for _, m := range r.Messages {
				fmt.Fprintf(w, "    - %s\n", strconv.Quote(m))
			}
		}
		fmt.Fprintf(w, "  ...\n")
	}
	return nil
}

// tapEscape keeps a name on one line and stops '#' starting a directive.
func tapEscape(s string) string {
	s = strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
	return strings.ReplaceAll(s, "#", `\#`)
}
