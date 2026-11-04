"""Publish a configurable batch of tasks to one queue every interval."""

import itertools
import json
import os
import time
import uuid

import pika

from common import QUEUES, READY_FILE, close, connect, install_signal_handlers, positive_integer, seconds


def main():
    install_signal_handlers()
    queue = os.environ["QUEUE_NAME"]
    if queue not in QUEUES:
        raise ValueError(f"QUEUE_NAME must be one of {QUEUES}")
    interval = seconds("INTERVAL_SECONDS", 5)
    batch_size = positive_integer("TASKS_PER_BATCH", 5)
    producer = f"{os.getenv('HOSTNAME', 'producer')}/{uuid.uuid4().hex[:8]}"
    sequence = itertools.count(1)
    connection = None
    try:
        connection, channel = connect()
        channel.confirm_delivery()
        READY_FILE.touch()
        print(f"{producer}: creating {batch_size} tasks for {queue} every {interval:g} seconds", flush=True)
        while True:
            started = time.monotonic()
            for _ in range(batch_size):
                task_id = next(sequence)
                task = {"task_id": task_id, "producer": producer, "queue": queue}
                channel.basic_publish(
                    exchange="",
                    routing_key=queue,
                    body=json.dumps(task).encode(),
                    properties=pika.BasicProperties(
                        content_type="application/json",
                        delivery_mode=2,
                        message_id=f"{producer}:{task_id}",
                    ),
                    mandatory=True,
                )
                print(f"created task #{task_id} for {queue} (producer={producer})", flush=True)
            # Pump AMQP heartbeats while waiting, including for long intervals.
            connection.sleep(max(0, interval - (time.monotonic() - started)))
    except KeyboardInterrupt:
        print("producer stopping", flush=True)
    finally:
        close(connection)


if __name__ == "__main__":
    main()
