package livefiles

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type platformFile struct {
	UUID      string `json:"file_uuid"`
	Preview   string `json:"preview_url"`
	Thumbnail string `json:"thumbnail_url"`
	Size      *int64 `json:"size_bytes"`
}

func (e *filesEnv) login(t *testing.T) []*http.Cookie {
	t.Helper()
	id := uuid.NewString()
	email := id + "@example.test"
	requireOK(t, e.database.WithPlatformAuthTx(e.ctx, func(tx db.PlatformAuthTxStore) error {
		user, err := tx.InsertUser(e.ctx, db.PlatformAuthUserInput{UUID: id, ExternalID: "user_" + id, OrganizationUUID: e.key.OrganizationUUID.String(), Email: email, Name: "File verification", Role: "admin"})
		if err != nil {
			return err
		}
		return tx.InsertWorkspaceMember(e.ctx, db.PlatformAuthWorkspaceMemberInput{ExternalID: "wmem_" + id, OrganizationUUID: e.key.OrganizationUUID.String(), WorkspaceUUID: e.key.WorkspaceUUID.String(), WorkspaceExternalID: "workspace_default", UserUUID: user.UUID, UserExternalID: "user_" + id, WorkspaceRole: "workspace_admin"})
	}))
	body, _ := json.Marshal(struct {
		Credentials map[string]string `json:"credentials"`
	}{map[string]string{"method": "code", "code": "123456", "email_address": email}})
	req, err := http.NewRequestWithContext(e.ctx, "POST", e.url+"/api/auth/verify_magic_link", bytes.NewReader(body))
	requireOK(t, err)
	req.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	requireOK(t, err)
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("login status=%d", response.StatusCode)
	}
	cookies := response.Cookies()
	for _, cookie := range cookies {
		if cookie.Name == "sessionKey" {
			return cookies
		}
	}
	t.Fatal("login session cookie missing")
	return nil
}

func (e *filesEnv) platformRequest(t *testing.T, method, route string, body []byte, cookies []*http.Cookie, status int) ([]byte, http.Header) {
	t.Helper()
	req, err := http.NewRequestWithContext(e.ctx, method, e.url+route, bytes.NewReader(body))
	requireOK(t, err)
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	requireOK(t, err)
	defer response.Body.Close()
	if response.StatusCode != status {
		t.Fatalf("platform %s status=%d want=%d", route, response.StatusCode, status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	requireOK(t, err)
	return data, response.Header
}

func TestFilesPlatform(t *testing.T) {
	e := newFilesEnv(t)
	cookies := e.login(t)
	route := "/api/" + e.key.OrganizationUUID.String() + "/upload_b64"
	e.platformRequest(t, "POST", route, []byte(`{}`), nil, 401)
	e.platformRequest(t, "POST", route, []byte(`{"file_b64":"??"}`), cookies, 400)
	e.platformRequest(t, "POST", route, []byte(`{"file_name":"bad:name","file_b64":"YQ=="}`), cookies, 400)
	tooLarge, _ := json.Marshal(map[string]string{"file_b64": base64.StdEncoding.EncodeToString(make([]byte, 65537))})
	e.platformRequest(t, "POST", route, tooLarge, cookies, 413)
	if len(e.objectKeys(t)) != 0 {
		t.Fatal("invalid platform uploads leaked objects")
	}
	e.proof(t, "platform_invalid_rejected")
	picture := image.NewRGBA(image.Rect(0, 0, 800, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 800; x++ {
			picture.Set(x, y, color.RGBA{R: 200, G: 30, B: 60, A: 255})
		}
	}
	var encoded bytes.Buffer
	requireOK(t, png.Encode(&encoded, picture))
	body, err := json.Marshal(map[string]string{"file_name": "image.png", "file_b64": base64.StdEncoding.EncodeToString(encoded.Bytes()), "file_kind": "image"})
	requireOK(t, err)
	data, _ := e.platformRequest(t, "POST", route, body, cookies, 200)
	var uploaded platformFile
	requireOK(t, json.Unmarshal(data, &uploaded))
	if uploaded.Size != nil || uploaded.UUID == "" {
		t.Fatal("platform metadata differs")
	}
	record, err := e.database.GetFileByUUIDInOrganization(e.ctx, e.key.OrganizationUUID.String(), uploaded.UUID)
	requireOK(t, err)
	if record.SizeBytes != int64(encoded.Len()) {
		t.Fatal("stored platform size differs")
	}
	for _, variant := range []string{"preview", "thumbnail"} {
		uri := "/api/" + e.key.OrganizationUUID.String() + "/files/" + uploaded.UUID + "/" + variant
		e.platformRequest(t, "GET", strings.Replace(uri, e.key.OrganizationUUID.String(), uuid.NewString(), 1), nil, cookies, 403)
	}
	preview, header := e.platformRequest(t, "GET", uploaded.Preview, nil, cookies, 200)
	if !bytes.Equal(preview, encoded.Bytes()) || header.Get("Content-Type") != "image/png" {
		t.Fatal("preview content differs")
	}
	thumbnail, header := e.platformRequest(t, "GET", uploaded.Thumbnail, nil, cookies, 200)
	thumb, err := png.Decode(bytes.NewReader(thumbnail))
	requireOK(t, err)
	if thumb.Bounds().Dx() != 400 || thumb.Bounds().Dy() != 200 || header.Get("Cache-Control") != "private, max-age=604800" {
		t.Fatal("thumbnail dimensions or cache contract differ")
	}
	if color.RGBAModel.Convert(thumb.At(0, 0)) != (color.RGBA{R: 200, G: 30, B: 60, A: 255}) || color.RGBAModel.Convert(thumb.At(399, 199)) != (color.RGBA{R: 200, G: 30, B: 60, A: 255}) {
		t.Fatal("thumbnail pixels differ")
	}
	variantKey := path.Dir(record.S3Key) + "/variants/thumbnail.png"
	e.assertObject(t, variantKey, thumbnail)
	e.proof(t, "preview_and_thumbnail_match")
	e.delete(t, record)
	if !e.objectAbsent(t, variantKey) || len(e.objectKeys(t)) != 0 {
		t.Fatal("thumbnail leaked after deletion")
	}
	e.platformRequest(t, "GET", uploaded.Preview, nil, cookies, 404)
	e.platformRequest(t, "GET", uploaded.Thumbnail, nil, cookies, 404)
	e.proof(t, "derived_objects_deleted")
}
