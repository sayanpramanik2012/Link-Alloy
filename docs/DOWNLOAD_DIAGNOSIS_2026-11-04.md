# Inconsistent Steam/EA download distribution — 0.0.3 alpha

Status: two scheduling defects reproduced and corrected. Performance acceptance has not passed. The user subsequently authorized pushing this work and publishing 0.0.3 as an alpha despite the unresolved performance issue.

## Live measurements and release decision

On 4 November, the local 0.0.3 build was running before Steam was launched. A 30-second pre-pause sample measured physical Ethernet receive rates of 100.90, 99.53, and 100.07 Mbps and Wi-Fi rates of 13.49, 10.41, and 11.70 Mbps. The virtual tunnel receive readings were 113.76, 109.22, and 111.22 Mbps. Adapter totals include protocol overhead and other applications. Steam's own log/UI reported approximately 105–109 Mbps. A socket snapshot showed eight content sockets per physical link to the two MAA content nodes, so connection placement alone no longer explained the byte imbalance in this run.

Steam was then paused through its UI. Isolated, interface-bound Cloudflare HTTPS tests measured Ethernet at 94.37 Mbps and Wi-Fi at 30.94 Mbps. A subsequent timed run measured Wi-Fi at 29.60 Mbps, Ethernet at 123.53 Mbps, and the system route through the tunnel at 86.87 Mbps. Source addresses were checked to confirm physical binding versus the virtual interface. Each sample was bounded to ten seconds and 128 MiB of counted payload, using four concurrent connections.

The corrected timed test keeps each worker offered work until the shared limit. An earlier fixed-size-per-worker tunnel sample (80.12 Mbps) is excluded from the performance comparison because the fast path could finish its assigned bytes early. Requests for a larger single object returned HTTP 403 with zero bytes; those attempts are failed tests, not zero-capacity measurements. The final run used the same supported-size requests as the initial successful test.

The corrected tunnel result was below the measured Ethernet-only result, so this is not a performance pass. Sequential, short samples and different CDN paths do not isolate engine overhead from endpoint/provider variation. Steam also logged failures to obtain fresh download sources, which remain an open investigation item. The protected-engine-log collection was cancelled at the Windows prompt. The session was interrupted before Steam was resumed, so no post-resume comparison was completed.

The user reviewed this verdict and explicitly authorized publication. The release is unsigned and marked prerelease; performance and recovery gates remain open.

## Evidence

The supplied screenshots show uneven payload rates despite equal 4:4 weights and healthy labels. Similar active connection counts are not evidence of similar useful download load. For example, 16 Wi-Fi connections could mostly be control/idle connections while one or two content connections transfer most bytes on Ethernet.

Initial read-only inspection found the then-running 0.0.2 app at `C:\Users\sayan\Downloads\Link.Alloy.Portable.exe`, with an app process and its recovery watcher. Both physical adapters and the Link Alloy tunnel were up. Steam's content log showed this download was paused at that point; no active-transfer CPU/disk or per-flow rate conclusion was drawn from that idle snapshot. The later measurements above used the local 0.0.3 build.

Steam log observations on 4 November, local time:

- 14:08:45: two MAA content nodes negotiated HTTPS/HTTP/2; Steam reported 73,923 remaining chunks.
- 14:08:57: target connection count increased to 9, with a reported 144.089 Mbps rate. This target is not a physical TCP socket count.
- 14:09:58: reported rate was 102.692 Mbps.
- 14:10:02: download was explicitly suspended. Subsequent zero-rate and idle-close entries cannot be interpreted as tunnel crashes.
- 14:11:15–14:11:30: the resumed session negotiated HTTP/2 to several MAA/HKG/SGP content nodes.
- 14:11:37: download was suspended again.

Different content-node sets and short measurement windows are additional variables. These records do not prove that the adapter, ISP, disk, or every CDN path can sustain a particular speed throughout the download. Protected engine logs and the elevated app's live diagnostics could not be read by the standard helper in this inspection, so the exact historical content-flow-to-link mapping was not recovered.

## Defect 1: service-blind scheduling

In 0.0.2, the cohort key is process creation identity + protocol + destination port + IP family. Steam login/background HTTPS and Steam content HTTPS can therefore share one rotation.

Reproduction: content, background, content, background, with two equal links. Overall assignments alternate correctly, but both content connections land on the same link. Before the fix, the test sent all 8 content connections to one of two links; the equivalent 3-link and 4-link tests concentrated all 12 and 16 content connections on one link as well.

Correction: inspect the initial TLS ClientHello on TCP 443/8443, derive its registrable service domain, and include that domain in the cohort. Steam content nodes share `steamcontent.com`; account/control services have their own rotation. The implementation is general and does not hard-code Steam, EA, or Epic host lists.

The original destination is unchanged. A bounded offline `crypto/tls` parser extracts only the visible handshake hostname. Its network writes and closes are disabled, parsing aborts before certificate/key exchange, and every consumed byte is forwarded unchanged. No certificate installation, application-data decryption, request rewriting, or synthetic download requests are used. Inspection is bounded to 64 KiB and 500 ms. Missing/unsupported SNI, encrypted inner names, partial/invalid TLS, and server-first protocols fall back to ordinary forwarding. This remains best-effort classification rather than a promise to identify every protocol or HTTPS origin.

References: [TLS server-name extension](https://www.rfc-editor.org/rfc/rfc6066.html), [Go public-suffix grouping](https://pkg.go.dev/golang.org/x/net/publicsuffix).

## Defect 2: unrelated health changes erase scheduling history

Previously, a change in either IP family's health cleared every cohort. An IPv6 probe change could therefore erase an IPv4 content pool's position while both IPv4 paths were still usable.

Before the fix, a repeated-probe/interleaved-background regression sent all 20 IPv4 content connections to one link. After the fix, that test distributes 10/10. Credits reset only when the particular cohort's eligible interface set or configured weights change. Normal monitor refreshes and unrelated family changes preserve rotation.

## Verification completed locally

- The two regressions failed on the earlier implementation, then passed after correction.
- Equal-weight service interleaving is checked across 2, 3, and 4 adapters.
- Real TLS handshake and payload forwarding pass against a local trusted test certificate with certificate verification enabled; the original connection target is checked by the injected dialer.
- Invalid/non-TLS prefix fallback preserves every byte. Server-first fallback returns within the inspection deadline and leaves the connection usable.
- Four persistent HTTP/2 sockets carry eight request rounds each, with payload verification, TLS verification, connection reuse, and a 2/2 path split. This is a loopback functional test, not a physical multi-ISP throughput test or an hours-long soak.
- Shutdown during handshake inspection releases the admitted worker. The inspection fuzz test passed 3,487 inputs, checking that parsing does not write to or close the original socket and preserves consumed bytes.
- Full Go tests, vet, frontend tests/build, and the native EXE build pass. The vulnerability scan reports zero reachable vulnerabilities and zero affected imported packages; it separately lists seven module-level findings in code paths the application does not call. Official Wintun hashes/signatures verify.
- The Connections screen can filter by service domain, making actual `steamcontent.com` bytes visible separately from account/control traffic. Service names remain in memory, not the event file.

## Further user acceptance test

1. Pause the download and fully exit Steam from its system-tray menu. Stop the old tunnel normally and close the old Link Alloy app.
2. Launch the 0.0.3 EXE as administrator. Verify the sidebar reads `0.0.3`.
3. Enable Ethernet and Wi-Fi at equal weights. Start the tunnel, reopen Steam, and resume the same download.
4. In Connections, filter `steamcontent.com`. Record substantial payload bytes and sustained rates per link over overlapping 60-second windows, not all Steam sockets together.
5. Repeat pause/resume and a launcher restart. Include longer uninterrupted download windows and watch the error/activity counters.
6. Repeat independently with EA; identify its service names from the diagnostics rather than assuming it uses the same domains or behavior as Steam.
7. If content connections are evenly assigned but one path remains slow, investigate that specific CDN/interface pair, including RTT, actual payload progress, server selection, and disk/CPU limits. Do not respond by randomly changing weights or terminating healthy sessions.

The implementation improves placement consistency in regression tests. A TLS/HTTP2 connection stays on its assigned link, and its request volume remains under the launcher's control. Equal weights do not guarantee equal bytes or full saturation of every link. CDN optimization and any relay-based single-flow bonding remain separate features. Do not call the performance issue resolved until the live comparison passes.

Short pause/resume cycles are not a clean reallocation test: the saved log shows Steam keeping pooled HTTP/2 sockets until they have been idle for 61 seconds. Reusing those sockets retains their original assignments. Fully exiting the launcher is the clearest initial test of the new code; subsequent pause/resume cycles test reuse stability rather than necessarily creating fresh sockets.
