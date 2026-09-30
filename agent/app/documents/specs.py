"""Structured schemas the LLM fills in for generated documents.

Asking the model for JSON matching a schema (instead of free text parsed with
regexes) means malformed output is caught by validation rather than silently
producing a broken file.
"""

from typing import Literal

from pydantic import BaseModel, Field


class Section(BaseModel):
    heading: str = Field(description="Section heading, plain text")
    paragraphs: list[str] = Field(default_factory=list, description="1-3 paragraphs of plain text")
    bullets: list[str] = Field(default_factory=list, description="Optional bullet points")


class DocumentSpec(BaseModel):
    title: str
    subtitle: str = ""
    introduction: str
    sections: list[Section] = Field(min_length=2, max_length=10)
    conclusion: str


class Stat(BaseModel):
    label: str
    value: str = Field(description="Short figure such as '42%' or '10M+'")


class Slide(BaseModel):
    type: Literal["bullets", "stats", "conclusion"]
    title: str
    bullets: list[str] = Field(default_factory=list, description="3-6 concise points")
    stats: list[Stat] = Field(default_factory=list, description="3-4 metrics, only for type=stats")


class DeckSpec(BaseModel):
    title: str
    subtitle: str = ""
    slides: list[Slide] = Field(min_length=3, max_length=14)
