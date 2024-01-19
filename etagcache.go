// Package etagcache provides middleware for caching HTTP responses based on the
// hash of the response body.
package etagcache

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"sync/atomic"
)

// UseETag returns a middleware that caches the response using the given etag
// value. If the client sends the same etag, the response is not generated
// again.
func UseETag(etag string, weak bool) func(next http.Handler) http.Handler {
	if weak {
		etag = "W/" + etag
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if checkIsCached(w, r, etag) {
				return
			}

			w.Header().Set("ETag", etag)
			next.ServeHTTP(w, r)
		})
	}
}

// UseAutomatic returns a middleware that caches the response using an etag
// value that is automatically generated from the response body.
// Note that this function requires the response body to be buffered so that it
// can obtain the hash value.
func UseAutomatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w2 := &bufferedResponseWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(w2, r)
		if w2.status == http.StatusNoContent || w2.status < 200 || w2.status >= 300 {
			w.WriteHeader(w2.status)
			w.Write(w2.b.Bytes())
			return
		}

		hash := sha256.Sum256(w2.b.Bytes())
		etag := base64.URLEncoding.EncodeToString(hash[:])

		if checkIsCached(w, r, etag) {
			return
		}

		w.Header().Set("ETag", etag)
		w.WriteHeader(w2.status)
		w.Write(w2.b.Bytes())
	})
}

type bufferedResponseWriter struct {
	http.ResponseWriter
	b      bytes.Buffer
	status int
}

func (w *bufferedResponseWriter) Write(b []byte) (int, error) {
	return w.b.Write(b)
}

func (w *bufferedResponseWriter) WriteHeader(status int) {
	w.status = status
}

// UseImmutable is similar to UseAutomatic, but it uses a hash value that is
// persisted after the first request. This is useful for immutable resources
// that are not changed during the application's lifetime.
func UseImmutable(next http.Handler) http.Handler {
	var cachedETag atomic.Pointer[string]
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if etagPtr := cachedETag.Load(); etagPtr != nil {
			if checkIsCached(w, r, *etagPtr) {
				return
			}
			w.Header().Set("ETag", *etagPtr)
			next.ServeHTTP(w, r)
			return
		}

		w2 := &bufferedResponseWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(w2, r)
		if w2.status == http.StatusNoContent || w2.status < 200 || w2.status >= 300 {
			w.WriteHeader(w2.status)
			w.Write(w2.b.Bytes())
			return
		}

		hash := sha256.Sum256(w2.b.Bytes())
		etag := base64.URLEncoding.EncodeToString(hash[:])
		cachedETag.Store(&etag)

		if checkIsCached(w, r, etag) {
			return
		}

		w.Header().Set("ETag", etag)
		w.WriteHeader(w2.status)
		w.Write(w2.b.Bytes())
	})
}

func checkIsCached(w http.ResponseWriter, r *http.Request, etag string) bool {
	ifNoneMatch := r.Header.Get("If-None-Match")
	if ifNoneMatch == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}
