import Foundation

actor SecretsAPI {
    private let baseURL: URL

    init(baseURL: URL = URL(string: "http://localhost:8082")!) {
        self.baseURL = baseURL
    }

    func fetchRiddle() async throws -> Riddle {
        let url = baseURL.appendingPathComponent("api/riddle")
        let (data, _) = try await URLSession.shared.data(from: url)
        return try JSONDecoder().decode(Riddle.self, from: data)
    }

    func submit(value: String, submittedBy: String) async throws -> SubmitResponse {
        let url = baseURL.appendingPathComponent("api/secrets")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        let body = ["value": value, "submitted_by": submittedBy]
        request.httpBody = try JSONEncoder().encode(body)

        let (data, _) = try await URLSession.shared.data(for: request)
        return try JSONDecoder().decode(SubmitResponse.self, from: data)
    }

    func listSecrets() async throws -> [Secret] {
        let url = baseURL.appendingPathComponent("api/secrets")
        let (data, _) = try await URLSession.shared.data(from: url)
        return try JSONDecoder().decode([Secret].self, from: data)
    }

    func fetchStats() async throws -> Stats {
        let url = baseURL.appendingPathComponent("api/stats")
        let (data, _) = try await URLSession.shared.data(from: url)
        return try JSONDecoder().decode(Stats.self, from: data)
    }
}
