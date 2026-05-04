// v0.15 — file upload + storage helpers for the /services/run runner.
//
// Storage layout on disk:
//   /storage/orders/<order_no>/<n>.<ext>
//
// Files are served back via the public route /storage/orders/...
// registered in main.go. Access control: order_no in the path acts as
// the secret. Anyone with the order URL can read its uploads. This
// matches the "URL is the access token" pattern on /services/run/<no>
// itself; we don't add a second auth layer here.
//
// Why local disk and not R2 / S3:
//   - Wedge is concierge mode for <50 customers in v0.15. Object
//     storage is over-engineering until either (a) we pass 50 customers
//     and the VPS disk fills up, or (b) we need CDN for global delivery.
//   - Docker volume already mounted via /opt/greentokey/data/coai/storage
//     so files persist across container rebuilds without extra ops.
//
// Image pipeline (v0.15 minimal):
//   1. Frontend posts multipart form with files under name="image"
//      (matches submitServiceRun in app/src/api/service-order.ts).
//   2. saveUploads validates type + size, writes to disk, returns
//      public URLs that NewAPI vision models can fetch.
//   3. executeAgentWithImages composes the OpenAI multimodal messages
//      array with text + image_url parts and fires the call.
//
// File limits are conservative — don't let a misbehaving customer or
// browser bug fill the VPS disk.

package service

import (
	"chat/globals"
	"crypto/sha256"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

const (
	uploadsRoot       = "/storage/orders"
	maxUploadBytes    = 8 << 20 // 8 MB per file (matches frontend hint)
	maxUploadsPerRun  = 12      // 12 image cap per upload batch
	uploadsURLPrefix  = "/storage/orders"
)

// allowedImageMIMEs gates which uploads we accept. Sniffed via
// http.DetectContentType (first 512 bytes), not the client-supplied
// Content-Type header.
var allowedImageMIMEs = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// saveUploads writes the multipart files to /storage/orders/<order_no>/
// and returns the publicly-fetchable URLs in submission order. Returns
// (nil, nil) if no files were attached.
func saveUploads(orderNo string, form *multipart.Form) ([]string, error) {
	if form == nil || form.File == nil {
		return nil, nil
	}

	// Build a flat list of headers across all field names. Frontend
	// uses field name "image"; tolerate other names too.
	var headers []*multipart.FileHeader
	for _, fhs := range form.File {
		headers = append(headers, fhs...)
		if len(headers) >= maxUploadsPerRun {
			break
		}
	}
	if len(headers) == 0 {
		return nil, nil
	}
	if len(headers) > maxUploadsPerRun {
		return nil, fmt.Errorf("too many files (max %d per run)", maxUploadsPerRun)
	}

	// Resolve dest dir. Use the storage root from config if present,
	// fall back to /storage. This lets dev environments override to a
	// tempdir without touching code.
	root := viper.GetString("storage.root")
	if root == "" {
		root = "/storage"
	}
	dir := filepath.Join(root, "orders", orderNo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}

	// Public base URL (so OpenAI / Anthropic vision models can fetch).
	// Defaults to the customer-facing gateway. Operator can override
	// via storage.public_base_url for split-host deployments.
	publicBase := viper.GetString("storage.public_base_url")
	if publicBase == "" {
		publicBase = "https://www.greentokey.com"
	}
	publicBase = strings.TrimRight(publicBase, "/")

	urls := make([]string, 0, len(headers))
	for i, fh := range headers {
		if fh.Size <= 0 {
			continue
		}
		if fh.Size > maxUploadBytes {
			return nil, fmt.Errorf("file %q too large (%d bytes; max %d)",
				fh.Filename, fh.Size, maxUploadBytes)
		}
		ext, err := sniffImageExt(fh)
		if err != nil {
			return nil, err
		}
		// Filename: <index>-<8-char-hash>.<ext>. Hash de-dupes a customer
		// uploading the same file twice; index keeps stable order.
		hash := shortHashFromHeader(fh)
		fname := fmt.Sprintf("%02d-%s%s", i+1, hash, ext)
		fpath := filepath.Join(dir, fname)

		if err := writeUploadToDisk(fh, fpath); err != nil {
			return nil, fmt.Errorf("save %s: %w", fname, err)
		}

		url := fmt.Sprintf("%s%s/%s/%s", publicBase, uploadsURLPrefix, orderNo, fname)
		urls = append(urls, url)
	}

	globals.Info(fmt.Sprintf("service: saved %d uploads for order %s", len(urls), orderNo))
	return urls, nil
}

// sniffImageExt returns the canonical extension for the file's true
// content type (ignoring whatever the client sent). Rejects non-image
// uploads — we don't want a customer using us as a free generic
// CDN.
func sniffImageExt(fh *multipart.FileHeader) (string, error) {
	f, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()

	head := make([]byte, 512)
	n, err := f.Read(head)
	if err != nil && err != io.EOF {
		return "", err
	}
	mime := http.DetectContentType(head[:n])
	ext, ok := allowedImageMIMEs[mime]
	if !ok {
		return "", fmt.Errorf("unsupported file type %q (allowed: jpg/png/webp/gif)", mime)
	}
	return ext, nil
}

// writeUploadToDisk streams the multipart file to the destination
// path. Uses a tmp file + rename for atomicity (so a crashed write
// doesn't leave a partial file readable to the agent).
func writeUploadToDisk(fh *multipart.FileHeader, dest string) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	tmpPath := dest + ".tmp"
	tmp, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, dest)
}

// shortHashFromHeader is a content-derived 8-char hash for the upload.
// Dedupes a customer uploading the same image twice, gives the file a
// stable name across retries.
func shortHashFromHeader(fh *multipart.FileHeader) string {
	f, err := fh.Open()
	if err != nil {
		return "00000000"
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "00000000"
	}
	sum := h.Sum(nil)
	return fmt.Sprintf("%x", sum[:4])
}

