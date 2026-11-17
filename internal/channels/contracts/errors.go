package contracts

import "errors"

var ErrMediaUploadRejected = errors.New("渠道媒体上传被明确拒绝，消息尚未发送")
