"""Errors that are safe to show to users."""


class AgentError(Exception):
    """An expected failure with a user-facing message.

    `code` is a stable identifier the frontend can switch on.
    """

    def __init__(self, status: int, code: str, message: str, title: str = "") -> None:
        super().__init__(message)
        self.status = status
        self.code = code
        self.message = message
        self.title = title or "Something went wrong"

    def to_dict(self) -> dict:
        return {"status": self.status, "code": self.code, "title": self.title, "message": self.message}


def not_found(message: str = "Conversation not found.") -> AgentError:
    return AgentError(404, "not_found", message, "Not found")


def bad_request(message: str) -> AgentError:
    return AgentError(400, "bad_request", message, "Invalid request")
