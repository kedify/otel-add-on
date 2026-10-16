"""Serve independent finite PI streams; capacity is a soft scaling target."""

import asyncio
import logging
import os
import signal
import socket

import grpc

from common import ProducerConfig, pi_digits
import stream_pb2
import stream_pb2_grpc


LOG = logging.getLogger("producer")


class Producer(stream_pb2_grpc.ProducerServicer):
    def __init__(self, config: ProducerConfig):
        self.config = config
        self.hostname = socket.gethostname()
        self.active = 0
        self.next_stream = 0

    async def Stream(self, request, context):
        stream_id = self.next_stream
        self.next_stream += 1
        self.active += 1
        count = self.config.count.sample()
        LOG.info("producer=%s stream=%s connected active=%s capacity=%s messages=%s",
                 self.hostname, stream_id, self.active, self.config.capacity, count)
        if self.active > self.config.capacity:
            LOG.warning("producer=%s soft capacity exceeded: active=%s capacity=%s",
                        self.hostname, self.active, self.config.capacity)
        try:
            await context.send_initial_metadata((("producer-pod", self.hostname),))
            for ordinal in range(count):
                if ordinal:
                    delay = self.config.interval.sample()
                    LOG.info("stream=%s next_delay_seconds=%s", stream_id, delay)
                    await asyncio.sleep(delay)
                digits = self.config.initial_digits + ordinal * self.config.digits_step
                pi = await asyncio.to_thread(pi_digits, digits)
                text = f"msg: {ordinal}: {pi}"
                LOG.info("producer=%s stream=%s %s", self.hostname, stream_id, text)
                yield stream_pb2.Message(text=text)
            LOG.info("stream=%s completed messages=%s", stream_id, count)
        finally:
            self.active -= 1
            LOG.info("producer=%s stream=%s closed active=%s",
                     self.hostname, stream_id, self.active)


async def serve():
    server = grpc.aio.server()
    stream_pb2_grpc.add_ProducerServicer_to_server(
        Producer(ProducerConfig.from_env()), server
    )
    address = os.getenv("PRODUCER_BIND_ADDRESS", "[::]:50051")
    if not server.add_insecure_port(address):
        raise RuntimeError(f"could not bind to {address}")
    stopped = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, stopped.set)
    await server.start()
    LOG.info("producer=%s listening on %s", socket.gethostname(), address)
    try:
        await stopped.wait()
    finally:
        await server.stop(grace=5)


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    asyncio.run(serve())
