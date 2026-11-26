import Foundation
import NearbyConnections

func makeNearby() -> any Nearby {
    NearbyPairing()
}

private struct Attempt {
    let id = UUID().uuidString
    let endpoint: EndpointID
    let device: String
    let incoming: Bool
    var verify: ((Bool) -> Void)?
    var approved = false
    var connected = false
}

private final class NearbyPairing: Nearby, AdvertiserDelegate, DiscovererDelegate, ConnectionManagerDelegate {
    private let queue = DispatchQueue(label: "local.lumiaqian.loopwall.pair")
    private lazy var manager = ConnectionManager(serviceID: "local.lumiaqian.loopwall.pair", strategy: .pointToPoint, queue: queue)
    private lazy var advertiser = Advertiser(connectionManager: manager)
    private lazy var discoverer = Discoverer(connectionManager: manager)
    private var device = ""
    private var advertising = false
    private var discovering = false
    private var endpoints: [EndpointID: String] = [:]
    private var active: Attempt?
    private var events: [NearbyEvent] = []

    private func failure(_ message: String) -> NSError {
        NSError(domain: "Loopwall.Nearby", code: 1, userInfo: [NSLocalizedDescriptionKey: message])
    }

    func configure(device: String, advertise: Bool, discover: Bool) {
        queue.sync {
            self.device = device
            manager.delegate = self
            advertiser.delegate = self
            discoverer.delegate = self
            if advertise != advertising {
                advertising = advertise
                if advertise {
                    advertiser.startAdvertising(using: Data(device.utf8), mediums: [.wifiLAN])
                } else {
                    advertiser.stopAdvertising()
                    if active?.incoming == true {
                        end()
                    }
                }
            }
            if discover != discovering {
                discovering = discover
                if discover {
                    discoverer.startDiscovery(mediums: [.wifiLAN])
                } else {
                    discoverer.stopDiscovery()
                    endpoints.removeAll()
                    if active?.incoming == false {
                        end()
                    }
                }
            }
        }
    }

    private func start(_ endpoint: EndpointID, device: String, incoming: Bool) {
        let attempt = Attempt(endpoint: endpoint, device: device, incoming: incoming)
        active = attempt
        queue.asyncAfter(deadline: .now() + 60) { [weak self] in
            if self?.active?.id == attempt.id {
                self?.end()
            }
        }
    }

    func request(device: String) throws {
        try queue.sync {
            let matches = endpoints.filter { $0.value == device }
            guard active == nil, matches.count == 1, let endpoint = matches.first?.key else {
                throw failure("未找到唯一可核对短码的设备，或已有配对正在进行")
            }
            start(endpoint, device: device, incoming: false)
            let id = active?.id
            discoverer.requestConnection(to: endpoint, using: Data(self.device.utf8), mediums: [.wifiLAN]) { error in
                if error != nil {
                    self.queue.async {
                        if self.active?.id == id {
                            self.end()
                        }
                    }
                }
            }
        }
    }

    private func emit(_ kind: String, _ value: String = "") {
        guard let a = active else { return }
        events.append(NearbyEvent(attempt: a.id, kind: kind, device: a.device, value: value, incoming: a.incoming))
    }

    private func end() {
        guard let a = active else { return }
        events.removeAll { $0.attempt == a.id }
        emit("ended")
        active = nil
        if let verify = a.verify {
            verify(false)
        }
        manager.disconnect(from: a.endpoint)
    }

    func decide(attempt: String, accept: Bool) throws {
        try queue.sync {
            guard let a = active, a.id == attempt else { throw failure("配对已结束") }
            guard accept else { end(); return }
            guard let verify = a.verify, !a.approved else { throw failure("确认码已失效") }
            active?.verify = nil
            active?.approved = true
            verify(true)
        }
    }

    func send(attempt: String, value: String) throws {
        try queue.sync {
            guard let a = active, a.id == attempt, a.connected else { throw failure("核对连接已关闭") }
            _ = manager.send(Data(value.utf8), to: [a.endpoint])
        }
    }

    func next() -> NearbyEvent {
        queue.sync {
            events.isEmpty ? NearbyEvent(attempt: "", kind: "", device: "", value: "", incoming: false) : events.removeFirst()
        }
    }

    func discoverer(_: Discoverer, didFind endpointID: EndpointID, with context: Data) {
        if context.count <= 64 {
            endpoints[endpointID] = String(data: context, encoding: .utf8)
        }
    }
    func discoverer(_: Discoverer, didLose endpointID: EndpointID) {
        endpoints.removeValue(forKey: endpointID)
    }

    func advertiser(
        _: Advertiser,
        didReceiveConnectionRequestFrom endpointID: EndpointID,
        with context: Data,
        connectionRequestHandler: @escaping (Bool) -> Void,
    ) {
        guard advertising, active == nil, context.count <= 64, let peer = String(data: context, encoding: .utf8),
              !peer.isEmpty, peer != device else { connectionRequestHandler(false); return }
        start(endpointID, device: peer, incoming: true)
        connectionRequestHandler(true)
    }

    func connectionManager(
        _: ConnectionManager,
        didReceive verificationCode: String,
        from endpointID: EndpointID,
        verificationHandler: @escaping (Bool) -> Void,
    ) {
        guard let a = active, a.endpoint == endpointID, a.verify == nil, !a.approved,
              !verificationCode.isEmpty else { verificationHandler(false); return }
        active?.verify = verificationHandler
        emit("verify", verificationCode)
    }

    func connectionManager(_: ConnectionManager, didChangeTo state: ConnectionState, for endpointID: EndpointID) {
        guard let a = active, a.endpoint == endpointID else { return }
        switch state {
        case .connected:
            guard a.approved else { end(); return }
            active?.connected = true
            emit("connected")
        case .disconnected, .rejected: end()
        case .connecting: break
        @unknown default: end()
        }
    }

    func connectionManager(_: ConnectionManager, didReceive data: Data, withID _: PayloadID, from endpointID: EndpointID) {
        guard let a = active, a.connected, a.endpoint == endpointID, data.count <= 1_024,
              let value = String(data: data, encoding: .utf8), !events.contains(where: { $0.kind == "payload" }) else { end(); return }
        emit("payload", value)
    }
    func connectionManager(
        _: ConnectionManager,
        didReceive _: InputStream,
        withID _: PayloadID,
        from _: EndpointID,
        cancellationToken token: CancellationToken,
    ) {
        token.cancel()
    }
    func connectionManager(
        _: ConnectionManager,
        didStartReceivingResourceWithID _: PayloadID,
        from _: EndpointID,
        at _: URL,
        withName _: String,
        cancellationToken token: CancellationToken,
    ) {
        token.cancel()
    }
    func connectionManager(
        _: ConnectionManager,
        didReceiveTransferUpdate _: TransferUpdate,
        from _: EndpointID,
        forPayload _: PayloadID,
    ) {}
}
