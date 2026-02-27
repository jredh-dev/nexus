import Foundation
import SwiftUI

@MainActor
class SecretsStore: ObservableObject {
    @Published var secrets: [Secret] = []
    @Published var riddle: Riddle?
    @Published var stats: Stats?
    @Published var lastResponse: SubmitResponse?
    @Published var errorMessage: String?
    @Published var isLoading = false

    private let api = SecretsAPI()

    func loadRiddle() async {
        do {
            riddle = try await api.fetchRiddle()
            stats = riddle?.stats
        } catch {
            errorMessage = "Cannot reach secrets service: \(error.localizedDescription)"
        }
    }

    func loadSecrets() async {
        do {
            secrets = try await api.listSecrets()
        } catch {
            errorMessage = "Failed to load secrets: \(error.localizedDescription)"
        }
    }

    func loadStats() async {
        do {
            stats = try await api.fetchStats()
        } catch {
            errorMessage = "Failed to load stats: \(error.localizedDescription)"
        }
    }

    func submit(value: String, as identity: String) async {
        isLoading = true
        errorMessage = nil
        defer { isLoading = false }

        do {
            lastResponse = try await api.submit(value: value, submittedBy: identity)
            await loadSecrets()
            await loadStats()
        } catch {
            errorMessage = "Submission failed: \(error.localizedDescription)"
        }
    }
}
