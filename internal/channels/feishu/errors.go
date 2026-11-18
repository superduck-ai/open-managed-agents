package feishu

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
)

var errChannelImageUploadRejected = errors.New("图片上传被明确拒绝，可以改为文件发送")

func feishuMediaUploadError(status, code int, image, permission bool) error {
	err := fmt.Errorf("飞书媒体上传失败，HTTP=%d code=%d", status, code)
	if status < 200 || status >= 500 || status == http.StatusRequestTimeout {
		return err
	}
	err = fmt.Errorf("%w: %w", contracts.ErrMediaUploadRejected, err)
	if image && status != http.StatusForbidden && code != 99991672 && code != 99991676 && code != 99991679 && !permission {
		return errors.Join(err, errChannelImageUploadRejected)
	}
	return err
}
