#!/usr/bin/env python3
# Source: kedify/github-meta/.github/scripts/rotate_kubernetes.py
# Update the source first, then refresh the public workflow copies.
"""Rotate a pinned Kubernetes CI manifest after the whole provider window exists."""

import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import sys
from urllib.error import HTTPError
from urllib.request import Request, urlopen


IMAGE = re.compile(r"(rancher/k3s|kindest/node):v1\.(\d+)\.(\d+)(-k3s\d+)?@sha256:[0-9a-f]{64}\Z")
K3S = re.compile(r"v1\.(\d+)\.(\d+)\+k3s(\d+)\Z")
INDEX_TYPES = ("application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json")


def request(url, headers=None):
    headers = {"User-Agent": "kedify-kubernetes-versions", **(headers or {})}
    if url.startswith("https://api.github.com/") and os.environ.get("GH_TOKEN"):
        headers["Authorization"] = "Bearer " + os.environ["GH_TOKEN"]
    return urlopen(Request(url, headers=headers), timeout=60)


def get_json(url):
    with request(url) as response:
        return json.load(response)


def read_manifest(document):
    rows = document["versions"] if isinstance(document, dict) and set(document) == {"versions"} else document
    if not isinstance(rows, list) or not rows:
        raise ValueError("expected a nonempty image list or KPA versions object")
    existing, provider = {}, None
    for row in rows:
        image = row.get("image") if isinstance(row, dict) else row
        match = IMAGE.fullmatch(image) if isinstance(image, str) else None
        if not match or (match[1] == "rancher/k3s") != bool(match[4]):
            raise ValueError(f"expected a digest-pinned kind or k3s image: {image!r}")
        minor = int(match[2])
        if minor in existing or provider not in (None, match[1]):
            raise ValueError("manifest must have distinct minors from one provider")
        if isinstance(document, dict):
            if not isinstance(row, dict) or set(row) != {"minor", "image", "envtest", "functional", "resilience"}:
                raise ValueError("invalid KPA version row")
            if row["minor"] != f"1.{minor}" or not re.fullmatch(rf"1\.{minor}\.\d+", row["envtest"]):
                raise ValueError("KPA minor/envtest must match the image")
            if any(type(row[key]) is not bool for key in ("functional", "resilience")):
                raise ValueError("KPA suite flags must be booleans")
        elif not isinstance(row, str):
            raise ValueError("image lists must contain strings")
        existing[minor], provider = row, match[1]
    return existing, provider


def upstream_minor():
    with request("https://dl.k8s.io/release/stable.txt") as response:
        version = response.read().decode().strip()
    match = re.fullmatch(r"v1\.(\d+)\.\d+", version)
    if not match:
        raise ValueError(f"unexpected upstream stable version: {version!r}")
    return int(match[1])


def kind_images(wanted):
    release = get_json("https://api.github.com/repos/kubernetes-sigs/kind/releases/latest")
    images = re.findall(r"kindest/node:v1\.\d+\.\d+@sha256:[0-9a-f]{64}", release["body"])
    available = {}
    for image in images:
        match = IMAGE.fullmatch(image)
        minor = int(match[2])
        previous = available.get(minor)
        if minor in wanted and (previous is None or int(IMAGE.fullmatch(previous)[3]) < int(match[3])):
            available[minor] = image
    return available


def dockerhub_image(tag, token):
    try:
        with request(f"https://registry-1.docker.io/v2/rancher/k3s/manifests/{tag}",
                     {"Authorization": "Bearer " + token, "Accept": ", ".join(INDEX_TYPES)}) as response:
            data = response.read()
            digest = response.headers.get("Docker-Content-Digest")
    except HTTPError as exc:
        if exc.code == 404:
            exc.close()
            return None
        raise
    index = json.loads(data)
    platforms = {(item.get("platform", {}).get("os"), item.get("platform", {}).get("architecture"))
                 for item in index.get("manifests", [])}
    if index.get("mediaType") not in INDEX_TYPES or not {("linux", "amd64"), ("linux", "arm64")} <= platforms:
        raise ValueError(f"rancher/k3s:{tag} is not a linux amd64/arm64 image index")
    if digest != "sha256:" + hashlib.sha256(data).hexdigest():
        raise ValueError(f"DockerHub digest mismatch for rancher/k3s:{tag}")
    return f"rancher/k3s:{tag}@{digest}"


def k3s_images(wanted):
    tags = {}
    # Recent releases cover the rolling window; do not scan all historical tags
    # while waiting for a provider to publish a newly released upstream minor.
    for page in range(1, 4):
        releases = get_json(f"https://api.github.com/repos/k3s-io/k3s/releases?per_page=100&page={page}")
        for release in releases:
            match = K3S.fullmatch(release["tag_name"])
            if release["draft"] or release["prerelease"] or not match:
                continue
            minor, patch, build = map(int, match.groups())
            if minor in wanted:
                tags.setdefault(minor, {})[(patch, build)] = release["tag_name"].replace("+", "-")
        if len(releases) < 100:
            break
    token = get_json("https://auth.docker.io/token?service=registry.docker.io&scope=repository:rancher/k3s:pull")["token"]
    available = {}
    for minor, candidates in tags.items():
        for _, tag in sorted(candidates.items(), reverse=True):
            image = dockerhub_image(tag, token)
            if image:
                available[minor] = image
                break
    return available


def rotate(document, latest, count, available):
    existing, provider = read_manifest(document)
    if latest < max(existing):
        raise ValueError("upstream stable is older than the current manifest; refusing to downgrade")
    wanted = list(range(latest, latest - count, -1))
    for minor, image in available.items():
        match = IMAGE.fullmatch(image) if isinstance(image, str) else None
        if not match or match[1] != provider or int(match[2]) != minor or (provider == "rancher/k3s") != bool(match[4]):
            raise ValueError(f"provider supplied an invalid image for Kubernetes 1.{minor}: {image!r}")
    missing = [minor for minor in wanted if minor not in available]
    if missing:
        raise ValueError(f"Pending {provider} images for upstream Kubernetes 1.{latest}: "
                         + ", ".join(f"1.{minor}" for minor in missing)
                         + ". No manifest changes; rerun after the provider publishes the complete window.")
    if set(existing) == set(wanted) and (isinstance(document, dict) or list(existing) == wanted):
        return copy.deepcopy(document)
    rows = []
    for minor in wanted:
        if minor in existing:
            rows.append(copy.deepcopy(existing[minor]))
        elif isinstance(document, dict):
            rows.append({"minor": f"1.{minor}", "image": available[minor], "envtest": f"1.{minor}.0",
                         "functional": True, "resilience": True})
        else:
            rows.append(available[minor])
    return {"versions": rows} if isinstance(document, dict) else rows


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest", type=Path)
    parser.add_argument("--count", type=int, choices=(1, 3), required=True)
    args = parser.parse_args()
    try:
        if not args.manifest.resolve().is_relative_to(Path.cwd()):
            raise ValueError("manifest must be inside the repository checkout")
        document = json.loads(args.manifest.read_text())
        _, provider = read_manifest(document)
        latest = upstream_minor()
        print(f"Upstream stable Kubernetes: **1.{latest}**. Provider: `{provider}`.", flush=True)
        wanted = list(range(latest, latest - args.count, -1))
        available = kind_images(wanted) if provider == "kindest/node" else k3s_images(wanted)
        updated = rotate(document, latest, args.count, available)
        print("\nComplete provider window: " + ", ".join(f"1.{minor}" for minor in wanted) + ".")
        print("\nRotate CI minors only; existing image pins are retained for Renovate patch/digest updates.")
        print("No published support bounds or Go/envtest dependencies are changed. Support-gated repositories may require "
              "a separately reviewed support change; failing packaging checks intentionally block merge.")
        if isinstance(document, dict):
            print("New KPA rows enable both suites. Confirm their recorded manual envtest assets before a support campaign.")
        if updated != document:
            args.manifest.write_text(json.dumps(updated, indent=2) + "\n")
        return 0
    except (OSError, ValueError, KeyError, TypeError) as exc:
        print(f"\nKubernetes rotation failed: {exc}", flush=True)
        return 1


if __name__ == "__main__":
    sys.exit(main())
