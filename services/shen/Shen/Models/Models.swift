import Foundation

struct Secret: Codable, Identifiable {
    let id: String
    let value: String
    let submittedBy: String
    let state: String
    let exposedBy: String?
    let exposedVia: String?
    let createdAt: String
    let exposedAt: String?

    enum CodingKeys: String, CodingKey {
        case id, value, state
        case submittedBy = "submitted_by"
        case exposedBy = "exposed_by"
        case exposedVia = "exposed_via"
        case createdAt = "created_at"
        case exposedAt = "exposed_at"
    }

    var isTruth: Bool { state == "truth" }
    var isLie: Bool { state == "lie" }
}

struct SubmitResponse: Codable {
    let secret: Secret
    let wasNew: Bool
    let selfBetrayal: Bool?
    let message: String

    enum CodingKeys: String, CodingKey {
        case secret, message
        case wasNew = "was_new"
        case selfBetrayal = "self_betrayal"
    }
}

struct Riddle: Codable {
    let riddle: String
    let rules: [String]
    let hint: String
    let endpoint: String
    let stats: Stats
}

struct Stats: Codable {
    let total: Int
    let truths: Int
    let lies: Int
    let lenses: Int
}
