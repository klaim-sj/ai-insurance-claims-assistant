package server

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Timeout http.Server. Nilai pendek berlaku untuk SEMUA route; hanya route
// unggah dokumen yang memperpanjang batas baca per request (uploadBody).
const (
	ReadHeaderTimeout = 5 * time.Second
	ReadTimeout       = 15 * time.Second
	WriteTimeout      = 30 * time.Second
	IdleTimeout       = 60 * time.Second

	// DefaultUploadReadTimeout: batas baca body untuk route unggah (±51 MB).
	DefaultUploadReadTimeout = 2 * time.Minute
)

// NewHTTPServer membuat http.Server dengan timeout pendek standar aplikasi.
func NewHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: ReadHeaderTimeout,
		ReadTimeout:       ReadTimeout,
		WriteTimeout:      WriteTimeout,
		IdleTimeout:       IdleTimeout,
	}
}

const ctxUpload ctxKey = 200

func isUploadRequest(ctx context.Context) bool {
	v, _ := ctx.Value(ctxUpload).(bool)
	return v
}

// uploadBody berjalan sebelum csrf (yang membaca body). Hanya untuk POST
// multipart ke route unggah dari session yang sudah login:
//   - memperpanjang read deadline koneksi untuk request ini saja lewat
//     http.ResponseController (route lain tetap memakai ReadTimeout pendek);
//   - memperpanjang write deadline agar respons tetap bisa dikirim setelah
//     pembacaan body yang lama;
//   - membatasi body dengan http.MaxBytesReader sesuai batas total unggah.
//
// Klien anonim tidak mendapat perpanjangan maupun batas besar.
func (s *Server) uploadBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if r.Method != http.MethodPost || !isMultipart(r) || !uploadPathRe.MatchString(r.URL.Path) ||
			s.sm.GetInt64(ctx, sessUserID) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		rc := http.NewResponseController(w)
		readUntil := time.Now().Add(s.uploadReadTimeout)
		if err := rc.SetReadDeadline(readUntil); err != nil && !errors.Is(err, http.ErrNotSupported) {
			s.serverError(w, r, err)
			return
		}
		if err := rc.SetWriteDeadline(readUntil.Add(WriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			s.serverError(w, r, err)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, ctxUpload, true)))
	})
}
