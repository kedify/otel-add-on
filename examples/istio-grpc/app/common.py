"""Configuration and integer Chudnovsky binary splitting for the demo."""

from dataclasses import dataclass
from math import isqrt
import os
import random
import re
import sys


@dataclass(frozen=True)
class IntegerRange:
    low: int
    high: int

    @classmethod
    def parse(cls, value: str, minimum: int = 0):
        match = re.fullmatch(r"([0-9]+)(?:±([0-9]+))?", value)
        if not match:
            raise ValueError(f"expected an integer or base±spread, got {value!r}")
        base = int(match[1])
        spread = int(match[2] or 0)
        if base - spread < minimum:
            raise ValueError(f"every value in {value!r} must be >= {minimum}")
        return cls(base - spread, base + spread)

    def sample(self) -> int:
        return random.randint(self.low, self.high)


def positive_integer(value: str) -> int:
    if not re.fullmatch(r"[0-9]+", value) or int(value) < 1:
        raise ValueError(f"expected a positive integer, got {value!r}")
    return int(value)


@dataclass(frozen=True)
class ProducerConfig:
    interval: IntegerRange
    count: IntegerRange
    capacity: int
    initial_digits: int
    digits_step: int

    @classmethod
    def from_env(cls):
        return cls(
            IntegerRange.parse(os.getenv("MESSAGE_INTERVAL_SECONDS", "5±1")),
            IntegerRange.parse(os.getenv("MESSAGE_COUNT", "30±5"), minimum=1),
            positive_integer(os.getenv("PRODUCER_CAPACITY", "3")),
            positive_integer(os.getenv("PI_INITIAL_DIGITS", "100")),
            positive_integer(os.getenv("PI_DIGITS_STEP", "100")),
        )


def consumer_delay() -> IntegerRange:
    return IntegerRange.parse(os.getenv("CONSUMER_START_DELAY_SECONDS", "5±2"))


def binary_split(start: int, end: int):
    """Evaluate the Chudnovsky series using integer arithmetic.

    Adapted from kedify/kedr examples/cpu-throttle/manifest.yaml.
    """
    if end - start == 1:
        if start == 0:
            p = q = 1
        else:
            p = (6 * start - 5) * (2 * start - 1) * (6 * start - 1)
            q = start**3 * (640320**3 // 24)
        t = p * (13591409 + 545140134 * start)
        return p, q, -t if start % 2 else t

    middle = (start + end) // 2
    p1, q1, t1 = binary_split(start, middle)
    p2, q2, t2 = binary_split(middle, end)
    return p1 * p2, q1 * q2, q2 * t1 + p1 * t2


def pi_digits(count: int) -> str:
    """Return PI truncated to count decimal places, with ten guard digits."""
    if count < 1:
        raise ValueError("PI precision must be positive")
    # The precision is operator-controlled; allow output beyond Python's default
    # 4,300 digit integer-to-string limit.
    sys.set_int_max_str_digits(0)
    _, q, t = binary_split(0, (count + 10) // 14 + 2)
    scale = 10 ** (count + 10)
    sqrt_scaled = isqrt(10005 * scale * scale)
    scaled = 426880 * sqrt_scaled * q // t
    digits = str(scaled // 10**10)
    return f"{digits[0]}.{digits[1:]}"
