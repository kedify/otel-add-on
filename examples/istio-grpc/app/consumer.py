"""Receive finite streams, reconnecting through the producer Service after EOF."""

import asyncio
import logging
import os

import grpc

from common import consumer_delay
import stream_pb2
import stream_pb2_grpc


LOG = logging.getLogger("consumer")


async def consume():
    delay = consumer_delay().sample()
    LOG.info("waiting %s seconds before connecting", delay)
    await asyncio.sleep(delay)
    address = os.getenv("PRODUCER_ADDRESS", "producer:50051")
    while True:
        # Use a separate connection pool so a new channel makes a new connection.
        async with grpc.aio.insecure_channel(
            address,
            options=(
                ("grpc.enable_retries", 0),
                ("grpc.use_local_subchannel_pool", 1),
            ),
        ) as channel:
            # Bound connection establishment without imposing a deadline on the stream.
            await asyncio.wait_for(channel.channel_ready(), timeout=60)
            call = stream_pb2_grpc.ProducerStub(channel).Stream(stream_pb2.StreamRequest())
            metadata = await call.initial_metadata()
            LOG.info("connected to producer=%s", metadata.get("producer-pod", "unknown"))
            count = 0
            async for message in call:
                print(message.text, flush=True)
                count += 1
        LOG.info("stream closed messages=%s; reconnecting to %s", count, address)


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    try:
        asyncio.run(consume())
    except (grpc.aio.AioRpcError, TimeoutError) as exc:
        LOG.error("stream failed: %s", exc)
        raise SystemExit(1)
