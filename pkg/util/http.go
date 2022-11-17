// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

func SendHTTPRequest(ctx context.Context, method, contentType, endpoint string, body io.Reader) ([]byte, error) {

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}
