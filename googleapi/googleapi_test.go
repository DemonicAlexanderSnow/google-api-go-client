// Copyright 2011 Google LLC. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package googleapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

type ExpandTest struct {
	in         string
	expansions map[string]string
	want       string
}

var expandTests = []ExpandTest{
	// no expansions
	{
		"http://www.golang.org/",
		map[string]string{},
		"http://www.golang.org/",
	},
	// one expansion, no escaping
	{
		"http://www.golang.org/{bucket}/delete",
		map[string]string{
			"bucket": "red",
		},
		"http://www.golang.org/red/delete",
	},
	// one expansion, with hex escapes
	{
		"http://www.golang.org/{bucket}/delete",
		map[string]string{
			"bucket": "red/blue",
		},
		"http://www.golang.org/red%2Fblue/delete",
	},
	// one expansion, with space
	{
		"http://www.golang.org/{bucket}/delete",
		map[string]string{
			"bucket": "red or blue",
		},
		"http://www.golang.org/red%20or%20blue/delete",
	},
	// expansion not found
	{
		"http://www.golang.org/{object}/delete",
		map[string]string{
			"bucket": "red or blue",
		},
		"http://www.golang.org//delete",
	},
	// multiple expansions
	{
		"http://www.golang.org/{one}/{two}/{three}/get",
		map[string]string{
			"one":   "ONE",
			"two":   "TWO",
			"three": "THREE",
		},
		"http://www.golang.org/ONE/TWO/THREE/get",
	},
	// utf-8 characters
	{
		"http://www.golang.org/{bucket}/get",
		map[string]string{
			"bucket": "£100",
		},
		"http://www.golang.org/%C2%A3100/get",
	},
	// punctuations
	{
		"http://www.golang.org/{bucket}/get",
		map[string]string{
			"bucket": `/\@:,.`,
		},
		"http://www.golang.org/%2F%5C%40%3A%2C./get",
	},
	// mis-matched brackets
	{
		"http://www.golang.org/{bucket/get",
		map[string]string{
			"bucket": "red",
		},
		"http://www.golang.org/%7Bbucket/get",
	},
	// "+" prefix for suppressing escape
	// See also: http://tools.ietf.org/html/rfc6570#section-3.2.3
	{
		"http://www.golang.org/{+topic}",
		map[string]string{
			"topic": "/topics/myproject/mytopic",
		},
		// The double slashes here look weird, but it's intentional
		"http://www.golang.org//topics/myproject/mytopic",
	},
}

func TestExpand(t *testing.T) {
	for i, test := range expandTests {
		u := url.URL{
			Path: test.in,
		}
		Expand(&u, test.expansions)
		got := u.EscapedPath()
		if got != test.want {
			t.Errorf("got %q expected %q in test %d", got, test.want, i+1)
		}
	}
}

func TestResolveRelative(t *testing.T) {
	resolveRelativeTests := []struct {
		basestr   string
		relstr    string
		want      string
		wantPanic bool
	}{
		{
			basestr: "http://www.golang.org/",
			relstr:  "topics/myproject/mytopic",
			want:    "http://www.golang.org/topics/myproject/mytopic",
		},
		{
			basestr: "http://www.golang.org/",
			relstr:  "topics/{+myproject}/{release}:build:test:deploy",
			want:    "http://www.golang.org/topics/{+myproject}/{release}:build:test:deploy",
		},
		{
			basestr: "https://www.googleapis.com/admin/reports/v1/",
			relstr:  "/admin/reports_v1/channels/stop",
			want:    "https://www.googleapis.com/admin/reports_v1/channels/stop",
		},
		{
			basestr: "https://www.googleapis.com/admin/directory/v1/",
			relstr:  "customer/{customerId}/orgunits{/orgUnitPath*}",
			want:    "https://www.googleapis.com/admin/directory/v1/customer/{customerId}/orgunits{/orgUnitPath*}",
		},
		{
			basestr: "https://www.googleapis.com/tagmanager/v2/",
			relstr:  "accounts",
			want:    "https://www.googleapis.com/tagmanager/v2/accounts",
		},
		{
			basestr: "https://www.googleapis.com/tagmanager/v2/",
			relstr:  "{+parent}/workspaces",
			want:    "https://www.googleapis.com/tagmanager/v2/{+parent}/workspaces",
		},
		{
			basestr: "https://www.googleapis.com/tagmanager/v2/",
			relstr:  "{+path}:create_version",
			want:    "https://www.googleapis.com/tagmanager/v2/{+path}:create_version",
		},
		{
			basestr:   "http://localhost",
			relstr:    ":8080foo",
			wantPanic: true,
		},
		{
			basestr: "https://www.googleapis.com/exampleapi/v2/somemethod",
			relstr:  "/upload/exampleapi/v2/somemethod",
			want:    "https://www.googleapis.com/upload/exampleapi/v2/somemethod",
		},
		{
			basestr: "https://otherhost.googleapis.com/exampleapi/v2/somemethod",
			relstr:  "/upload/exampleapi/v2/alternatemethod",
			want:    "https://otherhost.googleapis.com/upload/exampleapi/v2/alternatemethod",
		},
	}

	for _, test := range resolveRelativeTests {
		t.Run(test.basestr+test.relstr, func(t *testing.T) {
			if test.wantPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("expected panic, but did not see one")
					}
				}()
			}

			got := ResolveRelative(test.basestr, test.relstr)
			if got != test.want {
				t.Errorf("got %q expected %q", got, test.want)
			}
		})
	}
}

type CheckResponseTest struct {
	in       *http.Response
	bodyText string
	want     error
	errText  string
}

var checkResponseTests = []CheckResponseTest{
	{
		&http.Response{
			StatusCode: http.StatusOK,
		},
		"",
		nil,
		"",
	},
	{
		&http.Response{
			StatusCode: http.StatusInternalServerError,
		},
		`{"error":{}}`,
		&Error{
			Code: http.StatusInternalServerError,
			Body: `{"error":{}}`,
		},
		`googleapi: got HTTP response code 500 with body: {"error":{}}`,
	},
	{
		&http.Response{
			StatusCode: http.StatusNotFound,
		},
		`{"error":{"message":"Error message for StatusNotFound."}}`,
		&Error{
			Code:    http.StatusNotFound,
			Message: "Error message for StatusNotFound.",
			Body:    `{"error":{"message":"Error message for StatusNotFound."}}`,
		},
		"googleapi: Error 404: Error message for StatusNotFound.",
	},
	{
		&http.Response{
			StatusCode: http.StatusBadRequest,
		},
		`{"error":"invalid_token","error_description":"Invalid Value"}`,
		&Error{
			Code: http.StatusBadRequest,
			Body: `{"error":"invalid_token","error_description":"Invalid Value"}`,
		},
		`googleapi: got HTTP response code 400 with body: {"error":"invalid_token","error_description":"Invalid Value"}`,
	},
	{
		&http.Response{
			StatusCode: http.StatusBadRequest,
		},
		`{"error":{"errors":[{"domain":"usageLimits","reason":"keyInvalid","message":"Bad Request"}],"code":400,"message":"Bad Request"}}`,
		&Error{
			Code: http.StatusBadRequest,
			Errors: []ErrorItem{
				{
					Reason:  "keyInvalid",
					Message: "Bad Request",
				},
			},
			Body:    `{"error":{"errors":[{"domain":"usageLimits","reason":"keyInvalid","message":"Bad Request"}],"code":400,"message":"Bad Request"}}`,
			Message: "Bad Request",
		},
		"googleapi: Error 400: Bad Request, keyInvalid",
	},
	{
		&http.Response{
			StatusCode: http.StatusBadRequest,
		},
		`{"error": {"code": 400,"message": "The request has errors","status": "INVALID_ARGUMENT","details": [{"@type": "type.googleapis.com/google.rpc.BadRequest","fieldViolations": [{"field": "metadata.name","description": "The revision name must be prefixed by the name of the enclosing Service or Configuration with a trailing -"}]}]}}`,
		&Error{
			Code:    http.StatusBadRequest,
			Message: "The request has errors",
			Details: []interface{}{
				map[string]interface{}{
					"@type": "type.googleapis.com/google.rpc.BadRequest",
					"fieldViolations": []interface{}{
						map[string]interface{}{
							"field":       "metadata.name",
							"description": "The revision name must be prefixed by the name of the enclosing Service or Configuration with a trailing -",
						},
					},
				},
			},
			Body: `{"error": {"code": 400,"message": "The request has errors","status": "INVALID_ARGUMENT","details": [{"@type": "type.googleapis.com/google.rpc.BadRequest","fieldViolations": [{"field": "metadata.name","description": "The revision name must be prefixed by the name of the enclosing Service or Configuration with a trailing -"}]}]}}`,
		},
		`googleapi: Error 400: The request has errors
Details:
[
  {
    "@type": "type.googleapis.com/google.rpc.BadRequest",
    "fieldViolations": [
      {
        "description": "The revision name must be prefixed by the name of the enclosing Service or Configuration with a trailing -",
        "field": "metadata.name"
      }
    ]
  }
]`,
	},
	{
		// Case: Confirm that response headers are propagated to the error.
		&http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     map[string][]string{"key1": {"value1"}, "key2": {"value2", "value3"}},
		},
		`{"error":{}}`,
		&Error{
			Code:   http.StatusInternalServerError,
			Body:   `{"error":{}}`,
			Header: map[string][]string{"key1": {"value1"}, "key2": {"value2", "value3"}},
		},
		`googleapi: got HTTP response code 500 with body: {"error":{}}`,
	},
	{
		// Case: Error in JSON object in body.
		&http.Response{
			StatusCode: http.StatusTooManyRequests,
		},
		`{"error":{"code": 429,"message": "Resource has been exhausted (e.g. check quota).","status": "RESOURCE_EXHAUSTED"}}`,
		&Error{
			Code:    http.StatusTooManyRequests,
			Message: "Resource has been exhausted (e.g. check quota).",
			Body:    `{"error":{"code": 429,"message": "Resource has been exhausted (e.g. check quota).","status": "RESOURCE_EXHAUSTED"}}`,
		},
		`googleapi: Error 429: Resource has been exhausted (e.g. check quota).`,
	},
	{
		// Case: Streaming error in JSON array in body.
		&http.Response{
			StatusCode: http.StatusTooManyRequests,
		},
		`[{"error":{"code": 429,"message": "Resource has been exhausted (e.g. check quota).","status": "RESOURCE_EXHAUSTED"}}]`,
		&Error{
			Code:    http.StatusTooManyRequests,
			Message: "Resource has been exhausted (e.g. check quota).",
			Body:    `[{"error":{"code": 429,"message": "Resource has been exhausted (e.g. check quota).","status": "RESOURCE_EXHAUSTED"}}]`,
		},
		`googleapi: Error 429: Resource has been exhausted (e.g. check quota).`,
	},
}

func TestCheckResponse(t *testing.T) {
	for _, test := range checkResponseTests {
		res := test.in
		if test.bodyText != "" {
			res.Body = io.NopCloser(strings.NewReader(test.bodyText))
		}
		g := CheckResponse(res)
		if !reflect.DeepEqual(g, test.want) {
			t.Errorf("CheckResponse: got %v, want %v", g, test.want)
			gotJSON, err := json.Marshal(g)
			if err != nil {
				t.Error(err)
			}
			wantJSON, err := json.Marshal(test.want)
			if err != nil {
				t.Error(err)
			}
			t.Errorf("json(got):  %q\njson(want): %q", string(gotJSON), string(wantJSON))
		}
		if g != nil && g.Error() != test.errText {
			t.Errorf("CheckResponse: unexpected error message.\nGot:  %q\nwant: %q", g, test.errText)
		}
	}
}

func TestCheckResponseWithBody(t *testing.T) {
	for _, test := range checkResponseTests {
		res := test.in
		var body []byte
		if test.bodyText != "" {
			body = []byte(test.bodyText)
		}
		g := CheckResponseWithBody(res, body)
		if !reflect.DeepEqual(g, test.want) {
			t.Errorf("CheckResponse: got %v, want %v", g, test.want)
			gotJSON, err := json.Marshal(g)
			if err != nil {
				t.Error(err)
			}
			wantJSON, err := json.Marshal(test.want)
			if err != nil {
				t.Error(err)
			}
			t.Errorf("json(got):  %q\njson(want): %q", string(gotJSON), string(wantJSON))
		}
		if g != nil && g.Error() != test.errText {
			t.Errorf("CheckResponse: unexpected error message.\nGot:  %q\nwant: %q", g, test.errText)
		}
	}
}

type VariantPoint struct {
	Type        string
	Coordinates []float64
}

type VariantTest struct {
	in     map[string]interface{}
	result bool
	want   VariantPoint
}

var coords = []interface{}{1.0, 2.0}

var variantTests = []VariantTest{
	{
		in: map[string]interface{}{
			"type":        "Point",
			"coordinates": coords,
		},
		result: true,
		want: VariantPoint{
			Type:        "Point",
			Coordinates: []float64{1.0, 2.0},
		},
	},
	{
		in: map[string]interface{}{
			"type":  "Point",
			"bogus": coords,
		},
		result: true,
		want: VariantPoint{
			Type: "Point",
		},
	},
}

func TestVariantType(t *testing.T) {
	for _, test := range variantTests {
		if g := VariantType(test.in); g != test.want.Type {
			t.Errorf("VariantType(%v): got %v, want %v", test.in, g, test.want.Type)
		}
	}
}

func TestConvertVariant(t *testing.T) {
	for _, test := range variantTests {
		g := VariantPoint{}
		r := ConvertVariant(test.in, &g)
		if r != test.result {
			t.Errorf("ConvertVariant(%v): got %v, want %v", test.in, r, test.result)
		}
		if !reflect.DeepEqual(g, test.want) {
			t.Errorf("ConvertVariant(%v): got %v, want %v", test.in, g, test.want)
		}
	}
}

func TestRoundChunkSize(t *testing.T) {
	type testCase struct {
		in   int
		want int
	}
	for _, tc := range []testCase{
		{0, 0},
		{256*1024 - 1, 256 * 1024},
		{256 * 1024, 256 * 1024},
		{256*1024 + 1, 2 * 256 * 1024},
	} {
		mo := &MediaOptions{}
		ChunkSize(tc.in).setOptions(mo)
		if got := mo.ChunkSize; got != tc.want {
			t.Errorf("rounding chunk size: got: %v; want %v", got, tc.want)
		}
	}
}
