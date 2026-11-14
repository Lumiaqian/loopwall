import dnssd
import Foundation
import Network

/// Bonjour for sharing the library on the local network: the service this
/// Mac publishes for the manager's server, and the services of other Macs,
/// browsed for and resolved to an IPv4 address and port.
///
/// Everything it keeps is touched on `queue` only: the methods hop onto it,
/// and the browser, the registration and the connections run on it.
final class LocalSharing: @unchecked Sendable {
    static let shared = LocalSharing()
    private static let serviceType = "_phosphenego._tcp"

    private let queue = DispatchQueue(label: "local.lumiaqian.phosphenego.sharing")
    private var service: DNSServiceRef?
    private var browser: NWBrowser?
    /// Resolved peers, by service name.
    private var found: [String: Peer] = [:]
    /// Connections resolving a service, by its name.
    private var resolving: [String: NWConnection] = [:]

    func advertise(name: String, port: UInt16, device: String) throws {
        try queue.sync {
            withdraw()
            var txt = TXTRecordRef()
            TXTRecordCreate(&txt, 0, nil)
            defer { TXTRecordDeallocate(&txt) }
            let value = Array(device.utf8)
            guard value.count < 256, TXTRecordSetValue(&txt, "id", UInt8(value.count), value) == kDNSServiceErr_NoError else {
                throw Self.failure("无法生成 Bonjour 记录")
            }
            var ref: DNSServiceRef?
            let status = DNSServiceRegister(
                &ref, 0, 0, name, Self.serviceType, nil, nil, port.bigEndian,
                TXTRecordGetLength(&txt), TXTRecordGetBytesPtr(&txt), nil, nil,
            )
            guard status == kDNSServiceErr_NoError, let ref else {
                throw Self.failure("无法在局域网发布共享（\(status)）")
            }
            DNSServiceSetDispatchQueue(ref, queue)
            service = ref
        }
    }

    func stopAdvertising() {
        queue.sync { withdraw() }
    }

    func peers() -> [Peer] {
        queue.sync {
            if browser == nil {
                startBrowsing()
            }
            return found.values.sorted { $0.name < $1.name }
        }
    }

    /// On queue: a registration made with a dispatch queue is deallocated there.
    private func withdraw() {
        if let service {
            DNSServiceRefDeallocate(service)
        }
        service = nil
    }

    private func startBrowsing() {
        let browser = NWBrowser(for: .bonjourWithTXTRecord(type: Self.serviceType, domain: nil), using: .tcp)
        browser.browseResultsChangedHandler = { [weak self] results, _ in self?.update(results) }
        browser.stateUpdateHandler = { [weak self] state in
            // Denied the local network or otherwise failed: browse again on the next call.
            if case .failed = state, let self {
                self.browser?.cancel()
                self.browser = nil
            }
        }
        browser.start(queue: queue)
        self.browser = browser
    }

    /// On queue: resolves the services found again, as one restarted on
    /// another port keeps its name, and forgets those gone.
    private func update(_ results: Set<NWBrowser.Result>) {
        var names = Set<String>()
        for result in results {
            guard case let .service(name, _, _, _) = result.endpoint else { continue }
            names.insert(name)
            var device = ""
            if case let .bonjour(txt) = result.metadata {
                device = txt["id"] ?? ""
            }
            if resolving[name] == nil {
                resolve(name: name, device: device, endpoint: result.endpoint)
            }
        }
        for name in found.keys where !names.contains(name) {
            found[name] = nil
        }
        for (name, connection) in resolving where !names.contains(name) {
            connection.cancel()
            resolving[name] = nil
        }
    }

    /// On queue: connects to the service over IPv4 only to learn its address.
    private func resolve(name: String, device: String, endpoint: NWEndpoint) {
        let parameters = NWParameters.tcp
        if let ip = parameters.defaultProtocolStack.internetProtocol as? NWProtocolIP.Options {
            ip.version = .v4
        }
        let connection = NWConnection(to: endpoint, using: parameters)
        resolving[name] = connection
        connection.stateUpdateHandler = { [weak self, weak connection] state in
            guard let self, let connection else { return }
            switch state {
            case .ready:
                if case let .hostPort(host, port)? = connection.currentPath?.remoteEndpoint {
                    var address = "\(host)"
                    if let zone = address.firstIndex(of: "%") {
                        address = String(address[..<zone])
                    }
                    found[name] = Peer(name: name, device: device, host: address, port: Int64(port.rawValue))
                }
            case .failed, .waiting, .cancelled:
                break
            default:
                return
            }
            connection.cancel()
            if resolving[name] === connection {
                resolving[name] = nil
            }
        }
        connection.start(queue: queue)
    }

    private static func failure(_ message: String) -> NSError {
        NSError(domain: "PhospheneGo", code: 10, userInfo: [NSLocalizedDescriptionKey: message])
    }
}

extension NativeHost {
    func computerName() throws -> String {
        Foundation.Host.current().localizedName ?? ProcessInfo.processInfo.hostName
    }

    func advertise(name: String, port: Int64, device: String) throws {
        guard let port = UInt16(exactly: port), port > 0 else {
            throw NSError(domain: "PhospheneGo", code: 11, userInfo: [NSLocalizedDescriptionKey: "端口无效"])
        }
        try LocalSharing.shared.advertise(name: name, port: port, device: device)
    }

    func stopAdvertising() throws {
        LocalSharing.shared.stopAdvertising()
    }

    func peers() throws -> [Peer] {
        LocalSharing.shared.peers()
    }
}
