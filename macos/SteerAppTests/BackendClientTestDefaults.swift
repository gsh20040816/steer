// SPDX-License-Identifier: GPL-3.0-or-later

@testable import SteerApp

// Test backends only implement the calls their scenario exercises.
extension BackendClient {
    func parseWireGuard(document: String) async throws -> WireGuardImportResult { throw BackendClientError.helperUnavailable }
    func setEnabled(_ enabled: Bool) async throws -> (ApplyOutcome, ConfigurationSnapshot) {
        throw BackendClientError.helperUnavailable
    }
    func diagnostics() async throws -> ProbeDiagnostics { .empty }
    func probeResults() async throws -> ProbeLatestResults { .empty }
    func overviewState() async throws -> OverviewLifecycleState {
        let active = try await status()
        return OverviewLifecycleState(active: active)
    }
    func exportNode(node: JSONValue) async throws -> String {
        throw BackendClientError.helperUnavailable
    }
}
