"""Shared configuration and RabbitMQ connection setup for the demo workloads."""

import math
import os
from pathlib import Path
import signal
import time


QUEUES = ("example-queue1", "example-queue2")
READY_FILE = Path("/tmp/ready")


def seconds(name, default, allow_zero=False):
    value = float(os.getenv(name, str(default)))
    if not math.isfinite(value) or value < 0 or (value == 0 and not allow_zero):
        raise ValueError(f"{name} must be a finite {'nonnegative' if allow_zero else 'positive'} number")
    return value


def positive_integer(name, default):
    value = int(os.getenv(name, str(default)))
    if value < 1:
        raise ValueError(f"{name} must be a positive integer")
    return value


def install_signal_handlers():
    def stop(signum, frame):
        raise KeyboardInterrupt

    signal.signal(signal.SIGTERM, stop)
    READY_FILE.unlink(missing_ok=True)


def connect():
    import pika

    parameters = pika.ConnectionParameters(
        host=os.getenv("RABBITMQ_HOST", "rabbitmq"),
        port=int(os.getenv("RABBITMQ_PORT", "5672")),
        virtual_host="/",
        credentials=pika.PlainCredentials(
            os.environ["RABBITMQ_USER"], os.environ["RABBITMQ_PASSWORD"]
        ),
        heartbeat=30,
        socket_timeout=5,
        blocked_connection_timeout=30,
        client_properties={"connection_name": os.getenv("HOSTNAME", "rabbitmq-demo")},
    )
    while True:
        try:
            connection = pika.BlockingConnection(parameters)
            channel = connection.channel()
            for queue in QUEUES:
                channel.queue_declare(
                    queue=queue, durable=True, arguments={"x-queue-type": "classic"}
                )
            return connection, channel
        except pika.exceptions.AMQPConnectionError as error:
            print(f"waiting for RabbitMQ: {type(error).__name__}", flush=True)
            time.sleep(2)


def close(connection):
    READY_FILE.unlink(missing_ok=True)
    if connection is not None and connection.is_open:
        connection.close()
