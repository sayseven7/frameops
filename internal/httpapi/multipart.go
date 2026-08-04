package httpapi

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
)

const multipartOverheadBytes = 4 << 10

var errMultipartBodyTooLarge = errors.New("multipart body exceeds the accepted size")

func readMultipart(response http.ResponseWriter, request *http.Request, artifactLimit int64) (*multipart.Reader, error) {
	limit := artifactLimit + multipartOverheadBytes
	if request.ContentLength > limit {
		return nil, errMultipartBodyTooLarge
	}
	request.Body = http.MaxBytesReader(response, request.Body, limit)
	reader, err := request.MultipartReader()
	if err != nil {
		return nil, normalizeMultipartError(request, err)
	}
	return reader, nil
}

func normalizeMultipartError(request *http.Request, err error) error {
	_, drainErr := io.Copy(io.Discard, request.Body)
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) || errors.As(drainErr, &maxBytesError) {
		return errMultipartBodyTooLarge
	}
	return err
}
