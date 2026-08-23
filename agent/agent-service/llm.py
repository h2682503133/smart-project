"""agent-service：LLM 适配层（OpenAI 兼容，流式）。"""
from __future__ import annotations

from typing import Any, AsyncIterator

from shared.config import get_config


class LLMClient:
    def __init__(self) -> None:
        cfg = get_config("llm")
        self.base_url = cfg.get("base_url", "https://api.deepseek.com/v1")
        self.api_key = cfg.get("api_key")
        self.model = cfg.get("model", "deepseek-chat")
        self.temperature = float(cfg.get("temperature", 0.3))
        self.max_tokens = int(cfg.get("max_tokens", 1024))
        self._client: Any = None

    def _get_client(self):
        if self._client is None:
            from openai import AsyncOpenAI

            self._client = AsyncOpenAI(base_url=self.base_url, api_key=self.api_key)
        return self._client

    async def chat_stream(self, messages: list[dict[str, str]]) -> AsyncIterator[str]:
        """流式对话：逐条 yield 增量文本。"""
        client = self._get_client()
        stream = await client.chat.completions.create(
            model=self.model,
            messages=messages,
            temperature=self.temperature,
            max_tokens=self.max_tokens,
            stream=True,
        )
        async for chunk in stream:
            if chunk.choices and chunk.choices[0].delta and chunk.choices[0].delta.content:
                yield chunk.choices[0].delta.content

    async def chat(self, messages: list[dict[str, str]]) -> str:
        """非流式对话（意图分类、摘要生成等）。"""
        client = self._get_client()
        resp = await client.chat.completions.create(
            model=self.model,
            messages=messages,
            temperature=0.0,  # 意图/摘要用低温度保证稳定
        )
        return resp.choices[0].message.content or ""


_llm: LLMClient | None = None


def get_llm() -> LLMClient:
    global _llm
    if _llm is None:
        _llm = LLMClient()
    return _llm
