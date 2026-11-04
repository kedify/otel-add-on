"""Consume both queues, simulate work, then acknowledge each task."""

import json

from common import QUEUES, READY_FILE, close, connect, install_signal_handlers, seconds


def main():
    install_signal_handlers()
    work_seconds = seconds("WORK_SECONDS", 2, allow_zero=True)
    connection = None
    try:
        connection, channel = connect()
        # Each queue can prefetch one task. BlockingConnection dispatches the
        # callbacks sequentially, so a worker still processes one task at a time.
        channel.basic_qos(prefetch_count=1)

        def receive(channel, method, properties, body):
            task = json.loads(body)
            task_id, producer = task["task_id"], task["producer"]
            queue = method.routing_key
            print(
                f"received task #{task_id} from {queue}, I am working on it "
                f"(sleep {work_seconds:g} seconds).. (producer={producer})",
                flush=True,
            )
            connection.sleep(work_seconds)
            channel.basic_ack(delivery_tag=method.delivery_tag)
            print(f"task #{task_id} done (queue={queue}, producer={producer})", flush=True)

        for queue in QUEUES:
            channel.basic_consume(queue=queue, on_message_callback=receive, auto_ack=False)
        READY_FILE.touch()
        print(f"waiting for tasks from {', '.join(QUEUES)}; work={work_seconds:g} seconds", flush=True)
        channel.start_consuming()
    except KeyboardInterrupt:
        print("worker stopping; unfinished tasks will be requeued", flush=True)
    finally:
        close(connection)


if __name__ == "__main__":
    main()
