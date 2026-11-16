import dnssd
import Foundation
import Network
import os

private let log = Logger(subsystem: "local.lumiaqian.loopwall", category: "sharing")

/// Bonjour for sharing the library on the local network: the service this
/// Mac publishes for the manager's server, and the services of other Macs,
/// browsed for and resolved to an IPv4 address and port.
///
/// Bonjour needs the local network permission, and the system may refuse an
/// operation before the user answers its alert: so publishing reports what
/// Bonjour answers and tries again until it is confirmed, resolving waits
/// for the network rather than giving up, and what fails shows in `status`.
///
/// Everything it keeps is touched on `queue` only: the methods hop onto it,
/// and the browser, the registration and the connections run on it.
final class LocalSharing: @unchecked Sendable {
    static let shared = LocalSharing()
    private static let serviceType = "_loopwall._tcp"
    /// How long a registration or a resolve may go unanswered before it is retried.
    private static let patience: TimeInterval = 10

    private let queue = DispatchQueue(label: "local.lumiaqian.loopwall.sharing")

    private struct Registration { var name: String; var port: UInt16; var device: String }
    /// What to publish, nil when not sharing.
    private var registration: Registration?
    private var service: DNSServiceRef?
    private var retry: DispatchWorkItem?

    private var browser: NWBrowser?
    /// The services browsed, by name: their device and endpoint.
    private var results: [String: (device: String, endpoint: NWEndpoint)] = [:]
    /// Resolved peers, by service name.
    private var found: [String: Peer] = [:]
    /// Connections resolving a service, by its name.
    private var resolving: [String: NWConnection] = [:]

    private var current = SharingStatus(advertised: false, advertiseError: "", browsing: "", browseError: "", denied: false)
    private var advertiseDenied = false, browseDenied = false

    func advertise(name: String, port: UInt16, device: String) throws {
        guard device.utf8.count < 256 else {
            throw Self.failure("设备标识过长")
        }
        queue.sync {
            registration = Registration(name: name, port: port, device: device)
            register()
        }
    }

    func stopAdvertising() {
        queue.sync {
            registration = nil
            retry?.cancel()
            withdraw()
            current.advertised = false
            current.advertiseError = ""
            advertiseDenied = false
        }
    }

    func peers() -> [Peer] {
        queue.sync {
            if browser == nil {
                startBrowsing()
            }
            // Resolve again what failed or timed out.
            for (name, result) in results where found[name] == nil && resolving[name] == nil {
                resolve(name: name, device: result.device, endpoint: result.endpoint)
            }
            return found.values.sorted { $0.name < $1.name }
        }
    }

    func status() -> SharingStatus {
        queue.sync {
            var status = current
            status.denied = advertiseDenied || browseDenied
            return status
        }
    }

    // MARK: Publishing, on queue

    private func register() {
        retry?.cancel()
        withdraw()
        guard let registration else { return }
        current.advertised = false
        var txt = TXTRecordRef()
        TXTRecordCreate(&txt, 0, nil)
        defer { TXTRecordDeallocate(&txt) }
        let value = Array(registration.device.utf8)
        _ = TXTRecordSetValue(&txt, "id", UInt8(value.count), value)
        var ref: DNSServiceRef?
        let status = DNSServiceRegister(
            &ref, 0, 0, registration.name, Self.serviceType, nil, nil, registration.port.bigEndian,
            TXTRecordGetLength(&txt), TXTRecordGetBytesPtr(&txt),
            { _, _, error, _, _, _, context in
                guard let context else { return }
                Unmanaged<LocalSharing>.fromOpaque(context).takeUnretainedValue().registered(error)
            },
            Unmanaged.passUnretained(self).toOpaque(),
        )
        guard status == kDNSServiceErr_NoError, let ref else {
            registered(status)
            return
        }
        DNSServiceSetDispatchQueue(ref, queue)
        service = ref
        log.notice("registering \(registration.name, privacy: .public) on port \(registration.port)")
        // Unconfirmed after a while, as one the system held for the
        // permission alert: register again.
        schedule(after: Self.patience) { [weak self] in
            guard let self, !current.advertised else { return }
            log.notice("registration unconfirmed; registering again")
            register()
        }
    }

    /// Bonjour's answer to the registration, on queue.
    private func registered(_ error: DNSServiceErrorType) {
        if error == kDNSServiceErr_NoError {
            retry?.cancel()
            current.advertised = true
            current.advertiseError = ""
            advertiseDenied = false
            log.notice("registered")
            return
        }
        current.advertised = false
        current.advertiseError = Self.describe(error)
        advertiseDenied = error == kDNSServiceErr_PolicyDenied
        log.error("registration failed: \(error)")
        withdraw()
        schedule(after: 5) { [weak self] in self?.register() }
    }

    private func schedule(after delay: TimeInterval, _ work: @escaping () -> Void) {
        retry?.cancel()
        let item = DispatchWorkItem(block: work)
        retry = item
        queue.asyncAfter(deadline: .now() + delay, execute: item)
    }

    /// A registration made with a dispatch queue is deallocated there.
    private func withdraw() {
        if let service {
            DNSServiceRefDeallocate(service)
        }
        service = nil
    }

    // MARK: Browsing, on queue

    private func startBrowsing() {
        let browser = NWBrowser(for: .bonjourWithTXTRecord(type: Self.serviceType, domain: nil), using: .tcp)
        browser.browseResultsChangedHandler = { [weak self] results, _ in self?.update(results) }
        browser.stateUpdateHandler = { [weak self] state in self?.browserState(state) }
        browser.start(queue: queue)
        self.browser = browser
        log.notice("browsing")
    }

    private func browserState(_ state: NWBrowser.State) {
        switch state {
        case .ready:
            current.browsing = "ready"
            current.browseError = ""
            browseDenied = false
        case let .waiting(error):
            current.browsing = "waiting"
            current.browseError = Self.describe(error)
            browseDenied = Self.isDenied(error)
            log.notice("browser waiting: \(error, privacy: .public)")
        case let .failed(error):
            current.browsing = "failed"
            current.browseError = Self.describe(error)
            browseDenied = Self.isDenied(error)
            log.error("browser failed: \(error, privacy: .public)")
            // Browse again on the next call.
            browser?.cancel()
            browser = nil
        default:
            break
        }
    }

    /// Resolves the services found, as one restarted on another port keeps
    /// its name, and forgets those gone.
    private func update(_ browsed: Set<NWBrowser.Result>) {
        var names = Set<String>()
        for result in browsed {
            guard case let .service(name, _, _, _) = result.endpoint else { continue }
            names.insert(name)
            var device = ""
            if case let .bonjour(txt) = result.metadata {
                device = txt["id"] ?? ""
            }
            results[name] = (device, result.endpoint)
            if resolving[name] == nil {
                resolve(name: name, device: device, endpoint: result.endpoint)
            }
        }
        log.notice("browsed \(names.count) services")
        for name in results.keys where !names.contains(name) {
            results[name] = nil
            found[name] = nil
        }
        for (name, connection) in resolving where !names.contains(name) {
            connection.cancel()
            resolving[name] = nil
        }
    }

    /// Connects to the service over IPv4 only to learn its address. It
    /// waits for the network, as for the permission, up to a while.
    private func resolve(name: String, device: String, endpoint: NWEndpoint) {
        let parameters = NWParameters.tcp
        if let ip = parameters.defaultProtocolStack.internetProtocol as? NWProtocolIP.Options {
            ip.version = .v4
        }
        let connection = NWConnection(to: endpoint, using: parameters)
        resolving[name] = connection
        let done = { [weak self, weak connection] in
            guard let self, let connection else { return }
            connection.cancel()
            if resolving[name] === connection {
                resolving[name] = nil
            }
        }
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
                    log.notice("resolved \(name, privacy: .public) to \(address, privacy: .public):\(port.rawValue)")
                }
                done()
            case let .waiting(error):
                log.notice("resolving \(name, privacy: .public) waits: \(error, privacy: .public)")
            case let .failed(error):
                log.error("resolving \(name, privacy: .public) failed: \(error, privacy: .public)")
                done()
            default:
                break
            }
        }
        connection.start(queue: queue)
        queue.asyncAfter(deadline: .now() + Self.patience) { [weak connection] in
            if let connection, self.resolving[name] === connection {
                log.notice("resolving \(name, privacy: .public) timed out")
                done()
            }
        }
    }

    // MARK: Errors

    private static func isDenied(_ error: NWError) -> Bool {
        if case let .dns(code) = error {
            return code == kDNSServiceErr_PolicyDenied
        }
        return false
    }

    private static func describe(_ error: NWError) -> String {
        if case let .dns(code) = error {
            return describe(code)
        }
        return error.localizedDescription
    }

    private static func describe(_ code: DNSServiceErrorType) -> String {
        switch Int(code) {
        case Int(kDNSServiceErr_PolicyDenied):
            "本地网络权限未开启"
        case Int(kDNSServiceErr_NameConflict):
            "局域网中已有同名的服务"
        case Int(kDNSServiceErr_ServiceNotRunning):
            "系统的 Bonjour 服务没有运行"
        default:
            "Bonjour 错误（\(code)）"
        }
    }

    private static func failure(_ message: String) -> NSError {
        NSError(domain: "Loopwall", code: 10, userInfo: [NSLocalizedDescriptionKey: message])
    }
}

extension NativeHost {
    func computerName() throws -> String {
        Foundation.Host.current().localizedName ?? ProcessInfo.processInfo.hostName
    }

    func advertise(name: String, port: Int64, device: String) throws {
        guard let port = UInt16(exactly: port), port > 0 else {
            throw NSError(domain: "Loopwall", code: 11, userInfo: [NSLocalizedDescriptionKey: "端口无效"])
        }
        try LocalSharing.shared.advertise(name: name, port: port, device: device)
    }

    func stopAdvertising() throws {
        LocalSharing.shared.stopAdvertising()
    }

    func peers() throws -> [Peer] {
        LocalSharing.shared.peers()
    }

    func sharingStatus() throws -> SharingStatus {
        LocalSharing.shared.status()
    }
}
