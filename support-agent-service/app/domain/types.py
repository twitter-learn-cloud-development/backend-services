from typing import Annotated

from pydantic import StringConstraints

Identifier = Annotated[
    str,
    StringConstraints(min_length=1, max_length=128, pattern=r"^[A-Za-z0-9][A-Za-z0-9_.:-]*$"),
]
NonEmptyText = Annotated[str, StringConstraints(min_length=1, max_length=8_000)]
ShortText = Annotated[str, StringConstraints(min_length=1, max_length=2_000)]
