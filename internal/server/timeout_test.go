package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/claims"
	"github.com/klaim-sj/ai-insurance-claims-assistant/internal/testutil"
)

func TestNewHTTPServerShortTimeouts(t *testing.T) {
	hs := NewHTTPServer(":0", http.NotFoundHandler())
	if hs.ReadTimeout != 15*time.Second || hs.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("timeout server harus pendek: read=%v header=%v", hs.ReadTimeout, hs.ReadHeaderTimeout)
	}
	if hs.WriteTimeout <= 0 || hs.IdleTimeout <= 0 {
		t.Fatal("WriteTimeout dan IdleTimeout wajib diisi")
	}
}

// slowPost mengirim body secara bertahap selama ±total, sehingga pembacaan
// body melewati ReadTimeout server yang pendek.
func slowPost(c *http.Client, target, contentType string, body []byte, total time.Duration) (*http.Response, error) {
	pr, pw := io.Pipe()
	go func() {
		const chunks = 6
		step := (len(body) + chunks - 1) / chunks
		for i := 0; i < len(body); i += step {
			time.Sleep(total / chunks)
			if _, err := pw.Write(body[i:min(i+step, len(body))]); err != nil {
				return
			}
		}
		_ = pw.Close()
	}()
	req, _ := http.NewRequest(http.MethodPost, target, pr)
	req.Header.Set("Content-Type", contentType)
	resp, err := c.Do(req)
	_ = pr.CloseWithError(io.ErrClosedPipe)
	if err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	return resp, err
}

func multipartUpload(token string, data []byte) (string, []byte) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("csrf_token", token)
	_ = mw.WriteField("document_type", "lainnya")
	fw, _ := mw.CreateFormFile("files", "lambat.pdf")
	_, _ = fw.Write(data)
	_ = mw.Close()
	return mw.FormDataContentType(), buf.Bytes()
}

// Route lain tetap memakai ReadTimeout pendek; hanya route unggah (session
// login) yang memperpanjang read deadline per request.
func TestReadDeadlineExtendedOnlyForUpload(t *testing.T) {
	e := newEnv(t, nil)
	c, cl, token := e.officerClaim()

	const shortRead = 300 * time.Millisecond
	const slow = 1200 * time.Millisecond // > shortRead
	ts := httptest.NewUnstartedServer(e.srv.Handler())
	ts.Config.ReadHeaderTimeout = shortRead
	ts.Config.ReadTimeout = shortRead
	ts.Config.WriteTimeout = 5 * time.Second
	ts.Start()
	t.Cleanup(ts.Close)

	// 1) Route biasa (transisi status) dengan body lambat: gagal, status klaim tetap.
	form := url.Values{"csrf_token": {token}, "to": {string(claims.StatusSubmitted)}, "reason": {""}}.Encode()
	resp, err := slowPost(c, fmt.Sprintf("%s/claims/%d/status", ts.URL, cl.ID),
		"application/x-www-form-urlencoded", []byte(form), slow)
	if err == nil && resp.StatusCode == http.StatusSeeOther {
		t.Fatal("route non-unggah tidak boleh mendapat perpanjangan read deadline")
	}
	got, err := e.q.GetClaim(context.Background(), cl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != string(claims.StatusDraft) {
		t.Fatalf("status klaim tidak boleh berubah, dapat %q", got.Status)
	}

	// Kontrol: request yang sama tanpa jeda berhasil, jadi kegagalan di atas
	// memang karena timeout, bukan karena isi form.
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/claims/%d/status", ts.URL, cl.ID), bytes.NewBufferString(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if resp, err := c.Do(req); err != nil || resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("kontrol: transisi tanpa jeda harus 303, dapat resp=%v err=%v", resp, err)
	} else {
		resp.Body.Close()
	}

	// 2) Route unggah dengan body lambat yang sama: berhasil.
	ct, body := multipartUpload(token, testutil.PDF(1))
	target := fmt.Sprintf("%s/claims/%d/documents", ts.URL, cl.ID)
	resp, err = slowPost(c, target, ct, body, slow)
	if err != nil || resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unggah lambat harus berhasil (303), dapat resp=%v err=%v", resp, err)
	}
	if n := len(e.docs(cl.ID)); n != 1 {
		t.Fatalf("dokumen harus tersimpan, dapat %d", n)
	}

	// 3) Klien anonim ke route unggah: tidak mendapat perpanjangan.
	anon := e.client()
	ct, body = multipartUpload("x", testutil.PDF(2))
	resp, err = slowPost(anon, target, ct, body, slow)
	if err == nil && resp.StatusCode == http.StatusSeeOther {
		t.Fatal("klien anonim tidak boleh mendapat perpanjangan read deadline")
	}
	if n := len(e.docs(cl.ID)); n != 1 {
		t.Fatalf("tidak boleh ada dokumen baru, dapat %d", n)
	}
}
