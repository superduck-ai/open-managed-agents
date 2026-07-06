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
        "missing memory store",
        lambda: client.beta.memory_stores.retrieve("memstore_missing_python_sdk_e2e"),
        404,
    )

    store = client.beta.memory_stores.create(
        name="python-sdk-memory-store",
        description="python sdk memory e2e",
        metadata={"sdk": "python"},
    )
    assert store.type == "memory_store"
    assert store.metadata["sdk"] == "python"

    try:
        listed_stores = client.beta.memory_stores.list(limit=20)
        assert any(item.id == store.id for item in listed_stores.data), "store not found in list"

        retrieved_store = client.beta.memory_stores.retrieve(store.id)
        assert retrieved_store.id == store.id

        updated_store = client.beta.memory_stores.update(
            store.id,
            name="python-sdk-memory-store-updated",
            description="",
            metadata={"phase": "updated"},
        )
        assert updated_store.name == "python-sdk-memory-store-updated"
        assert updated_store.metadata["phase"] == "updated"

        memory = client.beta.memory_stores.memories.create(
            store.id,
            path="/python-sdk/notes.md",
            content="hello from python sdk memory",
            view="full",
        )
        assert memory.type == "memory"
        assert memory.memory_store_id == store.id
        assert memory.content == "hello from python sdk memory"
        first_version_id = memory.memory_version_id

        expect_status(
            "duplicate memory path",
            lambda: client.beta.memory_stores.memories.create(
                store.id,
                path="/python-sdk/notes.md",
                content="duplicate",
            ),
            409,
        )

        updated_memory = client.beta.memory_stores.memories.update(
            memory.id,
            memory_store_id=store.id,
            path="/python-sdk/renamed.md",
            content="updated from python sdk memory",
            view="full",
            precondition={"type": "content_sha256", "content_sha256": memory.content_sha256},
        )
        assert updated_memory.id == memory.id
        assert updated_memory.path == "/python-sdk/renamed.md"
        assert updated_memory.content == "updated from python sdk memory"
        assert updated_memory.memory_version_id != first_version_id

        retrieved_memory = client.beta.memory_stores.memories.retrieve(memory.id, memory_store_id=store.id)
        assert retrieved_memory.content == updated_memory.content

        memories = client.beta.memory_stores.memories.list(
            store.id,
            path_prefix="/python-sdk/",
            limit=20,
        )
        assert any(item.type == "memory" and item.id == memory.id for item in memories.data), "memory not found in list"

        versions = client.beta.memory_stores.memory_versions.list(
            store.id,
            memory_id=memory.id,
            limit=20,
        )
        assert any(item.operation == "created" for item in versions.data), "created version missing"
        assert any(item.operation == "modified" for item in versions.data), "modified version missing"

        first_version = client.beta.memory_stores.memory_versions.retrieve(
            first_version_id,
            memory_store_id=store.id,
            view="full",
        )
        assert first_version.content == memory.content

        redacted = client.beta.memory_stores.memory_versions.redact(first_version_id, memory_store_id=store.id)
        assert redacted.redacted_at is not None
        assert redacted.content_sha256 is None
        assert redacted.path is None

        deleted_memory = client.beta.memory_stores.memories.delete(
            memory.id,
            memory_store_id=store.id,
            expected_content_sha256=updated_memory.content_sha256,
        )
        assert deleted_memory.id == memory.id
        assert deleted_memory.type == "memory_deleted"
    finally:
        try:
            deleted_store = client.beta.memory_stores.delete(store.id)
            assert deleted_store.id == store.id
            assert deleted_store.type == "memory_store_deleted"
        except Exception:
            pass


if __name__ == "__main__":
    main()
