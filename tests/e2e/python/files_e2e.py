from __future__ import annotations

import os

import anthropic
from anthropic import Anthropic


BASE_URL = os.environ.get("TEST_API_BASE_URL", "http://127.0.0.1:18080")
API_KEY = os.environ.get("TEST_API_KEY", "sk-ant-local-default")


def expect_status(label: str, fn, status: int) -> None:
    try:
        fn()
    except anthropic.APIStatusError as exc:
        assert exc.status_code == status, f"{label}: got {exc.status_code}, want {status}"
        return
    raise AssertionError(f"{label}: expected request to fail with {status}")


def main() -> None:
    client = Anthropic(api_key=API_KEY, base_url=BASE_URL, max_retries=0)

    expect_status(
        "missing file metadata",
        lambda: client.beta.files.retrieve_metadata("file_missing_python_sdk_e2e"),
        404,
    )

    non_downloadable = client.beta.files.upload(
        file=("python-sdk-no-download.txt", b"python sdk no download", "text/plain")
    )
    try:
        expect_status("uploaded file download", lambda: client.beta.files.download(non_downloadable.id), 400)
    finally:
        try:
            client.beta.files.delete(non_downloadable.id)
        except Exception:
            pass

    uploaded = client.beta.files.upload(file=("python-sdk-e2e.txt", b"hello from python sdk", "text/plain"))
    assert uploaded.type == "file"
    assert uploaded.filename == "python-sdk-e2e.txt"

    page = client.beta.files.list(limit=20)
    assert any(file.id == uploaded.id for file in page.data), "uploaded file not found in list"

    retrieved = client.beta.files.retrieve_metadata(uploaded.id)
    assert retrieved.id == uploaded.id

    deleted = client.beta.files.delete(uploaded.id)
    assert deleted.id == uploaded.id
    assert deleted.type == "file_deleted"


if __name__ == "__main__":
    main()
