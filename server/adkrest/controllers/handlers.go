// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package controllers contains the controllers for the ADK REST API.
package controllers

import (
	"encoding/json"
	"log"
	"net/http"
)

// TODO: Move to an internal package, controllers doesn't have to be public API.

// EncodeJSONResponse uses the json encoder to write an interface to the http response with an optional status code
func EncodeJSONResponse(i any, status int, w http.ResponseWriter) {
	wHeader := w.Header()
	wHeader.Set("Content-Type", "application/json; charset=UTF-8")

	w.WriteHeader(status)

	if i != nil {
		err := json.NewEncoder(w).Encode(i)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

type errorHandler func(http.ResponseWriter, *http.Request) error

// trackingWriter records whether the handler wrote headers or body. Used so that
// handlers (e.g. SSE) that return an error after the response has started do not
// trigger a second WriteHeader via http.Error (which logs "superfluous
// response.WriteHeader" and hides the real error).
type trackingWriter struct {
	http.ResponseWriter
	started bool
}

func (tw *trackingWriter) WriteHeader(code int) {
	tw.started = true
	tw.ResponseWriter.WriteHeader(code)
}

func (tw *trackingWriter) Write(b []byte) (int, error) {
	tw.started = true
	return tw.ResponseWriter.Write(b)
}

// Unwrap preserves http.ResponseController / deadline behavior on the underlying writer.
func (tw *trackingWriter) Unwrap() http.ResponseWriter { return tw.ResponseWriter }

func (tw *trackingWriter) Flush() {
	if f, ok := tw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// NewErrorHandler writes the error code returned from the http handler.
func NewErrorHandler(fn errorHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tw := &trackingWriter{ResponseWriter: w}
		err := fn(tw, r)
		if err != nil {
			if tw.started {
				log.Printf("ERROR | adk | %s %s | handler error after response started (e.g. run_sse partial write): %v",
					r.Method, r.URL.Path, err)
				return
			}
			if statusErr, ok := err.(statusError); ok {
				http.Error(w, statusErr.Error(), statusErr.Status())
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}
	}
}

// Unimplemented returns 501 - Status Not Implemented error
func Unimplemented(rw http.ResponseWriter, req *http.Request) {
	rw.WriteHeader(http.StatusNotImplemented)
}
