# @twinsearth/aiprison

npm distribution of the **AI Prison** CLI — a defensive AI Agent governance tool:
tamper-evident audit chain, 5-step capability deprivation, accountable recovery
and cross-process ledger verification, implemented as a single zero-dependency Go binary.

## Install

```sh
npm install -g @twinsearth/aiprison
```

On install, a platform binary matching your OS/arch is downloaded from the
public GitHub Release (sha256-verified against `checksums.txt`). If you are
offline, the binary is skipped with a clear warning — `npm install` still
succeeds; install it later from <https://github.com/TwinsEarth/ai-prison/releases/latest>.

## Usage

```sh
aiprison version [-v]      # print version (+ injected commit/build time)
aiprison levels            # print the isolation/capability level ladder
aiprison demo              # run the built-in scenario demo
aiprison drill             # run the failure/recovery drill
aiprison inspect --data-dir DIR
aiprison verify  --data-dir DIR
aiprison repair-tail --data-dir DIR
```

## Supported platforms

| OS      | Architectures       | Asset                              |
| ------- | ------------------- | ---------------------------------- |
| linux   | amd64, arm64        | `aiprison_linux_<arch>.tar.gz`     |
| darwin  | amd64, arm64        | `aiprison_darwin_<arch>.tar.gz`    |
| windows | amd64, arm64        | `aiprison_windows_<arch>.zip`      |

Other platforms: build from source — see the
[repository README](https://github.com/TwinsEarth/ai-prison).

## Capability boundary

AI Prison is a **defensive governance prototype**. The execution backends
(GRAM / TLM credential stacks, hardware binding, cgroup quotas, real MicroVM
sandboxes, distributed revocation) are **simulated in memory** — strict by
design: traceability, verifiability and accountable recovery are real;
isolation enforcement is not.

## License

MIT — see the bundled `LICENSE` and <https://github.com/TwinsEarth/ai-prison>.
