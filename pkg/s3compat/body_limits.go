package s3compat

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
)

const maxS3ControlBodySize = 1 << 20

func readS3ControlBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	return io.ReadAll(http.MaxBytesReader(w, r.Body, maxS3ControlBodySize))
}

func decodeS3ControlXML(w http.ResponseWriter, r *http.Request, v any) error {
	if err := xml.NewDecoder(http.MaxBytesReader(w, r.Body, maxS3ControlBodySize)).Decode(v); err != nil {
		return fmt.Errorf("decode XML body: %w", err)
	}
	return nil
}
