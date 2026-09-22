package environments

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

type aliyunFlowImageBuilder struct {
	http        buildHTTP
	pipelineURL string
}
type aliyunFlowBuildRef struct {
	RunID       string `json:"run"`
	TaggedImage string `json:"image"`
}
type aliyunFlowJob struct {
	ID json.Number `json:"id"`
}
type aliyunFlowRun struct {
	Status string `json:"status"`
	Stages []struct {
		StageInfo struct {
			Jobs []aliyunFlowJob `json:"jobs"`
		} `json:"stageInfo"`
	} `json:"stages"`
}

func (f *aliyunFlowImageBuilder) Start(ctx context.Context, input imageBuildInput) (buildJobRef, error) {
	params, err := json.Marshal(struct {
		Envs map[string]string `json:"envs"`
	}{map[string]string{
		"DOCKERFILE_TEXT": "base64:" + base64.StdEncoding.EncodeToString([]byte(input.Dockerfile)),
		"IMAGE_REPO":      input.Repository,
		"IMAGE_TAG":       input.Tag,
	}})
	if err != nil {
		return "", err
	}
	var run json.Number
	err = f.http.call(ctx, http.MethodPost, f.pipelineURL+"/runs", struct {
		Params string `json:"params"`
	}{string(params)}, &run)
	if err != nil {
		return "", err
	}
	number, err := run.Int64()
	if err != nil || number <= 0 {
		return "", fmt.Errorf("Aliyun Flow returned an invalid run ID")
	}
	return encodeJobRef(aliyunFlowBuildRef{RunID: run.String(), TaggedImage: input.Repository + ":" + input.Tag}), nil
}

func (f *aliyunFlowImageBuilder) getRun(ctx context.Context, ref buildJobRef) (aliyunFlowBuildRef, aliyunFlowRun, error) {
	var job aliyunFlowBuildRef
	if err := json.Unmarshal([]byte(ref), &job); err != nil {
		return job, aliyunFlowRun{}, err
	}
	var run aliyunFlowRun
	err := f.http.call(ctx, http.MethodGet, f.pipelineURL+"/runs/"+url.PathEscape(job.RunID), nil, &run)
	return job, run, err
}

func (f *aliyunFlowImageBuilder) GetStatus(ctx context.Context, ref buildJobRef) (imageBuildStatus, error) {
	job, run, err := f.getRun(ctx, ref)
	if err != nil {
		return imageBuildStatus{}, err
	}
	status := imageBuildStatus{buildJobStatus: buildJobStatus{State: aliyunFlowState(run.Status), Message: run.Status}}
	if status.State == "succeeded" {
		// Each attempt uses a unique tag; the pipeline must not overwrite it.
		status.ImageRef = job.TaggedImage
	}
	return status, nil
}

func aliyunFlowState(status string) string {
	switch strings.ToUpper(status) {
	case "SUCCESS":
		return "succeeded"
	case "FAIL", "FAILED":
		return "failed"
	case "CANCELED", "CANCELLED":
		return "cancelled"
	case "WAITING":
		return "queued"
	default:
		return "running"
	}
}

func (f *aliyunFlowImageBuilder) Cancel(ctx context.Context, ref buildJobRef) error {
	var job aliyunFlowBuildRef
	if err := json.Unmarshal([]byte(ref), &job); err != nil {
		return err
	}
	var accepted bool
	if err := f.http.call(ctx, http.MethodPut, f.pipelineURL+"/runs/"+url.PathEscape(job.RunID), nil, &accepted); err != nil {
		return err
	}
	if !accepted {
		return fmt.Errorf("Aliyun Flow did not accept cancellation")
	}
	return nil
}

func (*aliyunFlowImageBuilder) Capabilities() buildCapabilities {
	return buildCapabilities{Logs: true, Cancel: true}
}

type aliyunFlowLogStep struct {
	StepIndex *int   `json:"stepIndex"`
	Finish    bool   `json:"finish"`
	Status    string `json:"status"`
}
type aliyunFlowStepGroup struct {
	BuildID json.Number         `json:"buildId"`
	Steps   []aliyunFlowLogStep `json:"steps"`
	Nodes   []aliyunFlowLogStep `json:"buildProcessNodes"`
}
type aliyunFlowLogCursor struct {
	Offsets map[string]int64 `json:"offsets"`
	Done    map[string]bool  `json:"done"`
}

func (f *aliyunFlowImageBuilder) ReadLogs(ctx context.Context, ref buildJobRef, cursor string) (buildLogChunk, error) {
	position := aliyunFlowLogCursor{Offsets: map[string]int64{}, Done: map[string]bool{}}
	if cursor != "" {
		if len(cursor) > 64<<10 {
			return buildLogChunk{}, errPrebuildCursor
		}
		data, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(data, &position) != nil || position.Offsets == nil || position.Done == nil {
			return buildLogChunk{}, errPrebuildCursor
		}
	}
	job, run, err := f.getRun(ctx, ref)
	if err != nil {
		return buildLogChunk{}, err
	}
	terminal := aliyunFlowState(run.Status) == "succeeded" || aliyunFlowState(run.Status) == "failed" || aliyunFlowState(run.Status) == "cancelled"
	for _, stage := range run.Stages {
		for _, task := range stage.StageInfo.Jobs {
			chunk, found, err := f.readJobLogs(ctx, job.RunID, task.ID.String(), terminal, &position)
			if err != nil {
				return buildLogChunk{}, err
			}
			if found {
				return chunk, nil
			}
		}
	}
	return buildLogChunk{NextCursor: encodeAliyunFlowLogCursor(position), Complete: terminal}, nil
}

func (f *aliyunFlowImageBuilder) readJobLogs(ctx context.Context, run, job string, terminal bool, position *aliyunFlowLogCursor) (buildLogChunk, bool, error) {
	base := f.pipelineURL + "/pipelineRuns/" + url.PathEscape(run) + "/jobs/" + url.PathEscape(job)
	var raw json.RawMessage
	if err := f.http.call(ctx, http.MethodGet, base+"/steps", nil, &raw); err != nil {
		return buildLogChunk{}, false, err
	}
	groups, err := decodeAliyunFlowSteps(raw)
	if err != nil {
		return buildLogChunk{}, false, err
	}
	for _, group := range groups {
		steps := group.Nodes
		if len(steps) == 0 {
			steps = group.Steps
		}
		for _, step := range steps {
			status := strings.ToUpper(step.Status)
			if step.StepIndex == nil || status == "SKIP" || status == "NOT_STARTED" {
				continue
			}
			key := job + "/" + group.BuildID.String() + "/" + strconv.Itoa(*step.StepIndex)
			if position.Done[key] {
				continue
			}
			offset := position.Offsets[key]
			if offset < 0 {
				return buildLogChunk{}, false, errPrebuildCursor
			}
			q := url.Values{"buildId": {group.BuildID.String()}, "stepIndex": {strconv.Itoa(*step.StepIndex)}}
			var value []byte
			if err := f.http.call(ctx, http.MethodGet, base+"/step/log/url?"+q.Encode(), nil, &value); err != nil {
				return buildLogChunk{}, false, err
			}
			address, err := aliyunFlowLogURL(value)
			if err != nil {
				return buildLogChunk{}, false, err
			}
			text, eof, err := readAliyunFlowLogDownload(ctx, f.http.client, address, offset)
			if err != nil {
				return buildLogChunk{}, false, err
			}
			position.Offsets[key] = offset + int64(len(text))
			finished := terminal || step.Finish || aliyunFlowState(step.Status) == "succeeded" || aliyunFlowState(step.Status) == "failed"
			if eof && finished {
				position.Done[key] = true
			}
			if offset == 0 && text != "" {
				text = "--- " + key + " ---\n" + text
			}
			return buildLogChunk{Text: text, NextCursor: encodeAliyunFlowLogCursor(*position)}, true, nil
		}
	}
	return buildLogChunk{}, false, nil
}

func decodeAliyunFlowSteps(data []byte) ([]aliyunFlowStepGroup, error) {
	var groups []aliyunFlowStepGroup
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("[")) {
		err := json.Unmarshal(data, &groups)
		return groups, err
	}
	var group aliyunFlowStepGroup
	if err := json.Unmarshal(data, &group); err != nil {
		return nil, err
	}
	return []aliyunFlowStepGroup{group}, nil
}

func aliyunFlowLogURL(data []byte) (string, error) {
	address := strings.TrimSpace(string(data))
	if !strings.HasPrefix(address, "https://") && json.Unmarshal(data, &address) != nil {
		var value struct {
			URL         string `json:"url"`
			DownloadURL string `json:"downloadUrl"`
		}
		if err := json.Unmarshal(data, &value); err != nil {
			return "", err
		}
		address = value.URL
		if address == "" {
			address = value.DownloadURL
		}
	}
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("Aliyun Flow log download is not available yet")
	}
	return address, nil
}

func encodeAliyunFlowLogCursor(position aliyunFlowLogCursor) string {
	data, _ := json.Marshal(position)
	return base64.RawURLEncoding.EncodeToString(data)
}

// Download URLs avoid guessing the step-log API's undocumented line offset rules.
// No Aliyun Flow credentials are sent to the download host. Range-ignorant servers work too.
func readAliyunFlowLogDownload(ctx context.Context, client *http.Client, address string, offset int64) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("Aliyun Flow log download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		if resp.Header.Get("Content-Range") != fmt.Sprintf("bytes */%d", offset) {
			return "", false, fmt.Errorf("Aliyun Flow log size changed; reopen logs from the beginning")
		}
		return "", true, nil
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return "", false, fmt.Errorf("Aliyun Flow log download returned HTTP %d", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusPartialContent && !strings.HasPrefix(resp.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", offset)) {
		return "", false, fmt.Errorf("Aliyun Flow log download returned an invalid range")
	}
	if resp.StatusCode == http.StatusOK && offset > 0 {
		if _, err := io.CopyN(io.Discard, resp.Body, offset); err != nil {
			return "", err == io.EOF, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil {
		return "", false, err
	}
	eof := len(data) <= 64<<10
	if !eof {
		data = data[:64<<10]
		for i := len(data) - 1; i >= max(0, len(data)-utf8.UTFMax); i-- {
			if utf8.RuneStart(data[i]) {
				if !utf8.FullRune(data[i:]) {
					data = data[:i]
				}
				break
			}
		}
	}
	return string(data), eof, nil
}
