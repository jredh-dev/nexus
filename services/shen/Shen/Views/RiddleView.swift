import SwiftUI

struct RiddleView: View {
    @EnvironmentObject var store: SecretsStore

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                if let riddle = store.riddle {
                    Text("The Riddle")
                        .font(.title.bold())

                    Text(riddle.riddle)
                        .font(.body)
                        .italic()
                        .padding()
                        .background(Color.secondary.opacity(0.1))
                        .cornerRadius(8)

                    Divider()

                    Text("Rules")
                        .font(.title2.bold())

                    ForEach(Array(riddle.rules.enumerated()), id: \.offset) { i, rule in
                        HStack(alignment: .top, spacing: 8) {
                            Text("\(i + 1).")
                                .font(.body.monospacedDigit())
                                .foregroundColor(.secondary)
                            Text(rule)
                                .font(.body)
                        }
                    }

                    Divider()

                    Text("Hint")
                        .font(.title3.bold())
                    Text(riddle.hint)
                        .font(.body)
                        .foregroundColor(.orange)

                    if let stats = store.stats {
                        Divider()
                        StatsBar(stats: stats)
                    }
                } else if let err = store.errorMessage {
                    VStack(spacing: 12) {
                        Image(systemName: "wifi.slash")
                            .font(.largeTitle)
                            .foregroundColor(.red)
                        Text(err)
                            .foregroundColor(.secondary)
                        Text("Make sure the secrets service is running on :8082")
                            .font(.caption)
                            .foregroundColor(.secondary)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    ProgressView("Loading riddle...")
                }
            }
            .padding()
        }
    }
}

struct StatsBar: View {
    let stats: Stats

    var body: some View {
        HStack(spacing: 24) {
            StatPill(label: "Total", value: stats.total, color: .blue)
            StatPill(label: "Truths", value: stats.truths, color: .green)
            StatPill(label: "Lies", value: stats.lies, color: .red)
            StatPill(label: "Lenses", value: stats.lenses, color: .purple)
        }
    }
}

struct StatPill: View {
    let label: String
    let value: Int
    let color: Color

    var body: some View {
        VStack(spacing: 2) {
            Text("\(value)")
                .font(.title2.bold().monospacedDigit())
                .foregroundColor(color)
            Text(label)
                .font(.caption)
                .foregroundColor(.secondary)
        }
    }
}
