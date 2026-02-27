import SwiftUI

struct SecretsListView: View {
    @EnvironmentObject var store: SecretsStore

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Secrets")
                    .font(.title.bold())
                Spacer()
                Button("Refresh") {
                    Task { await store.loadSecrets() }
                }
                .buttonStyle(.bordered)
            }

            if let stats = store.stats {
                StatsBar(stats: stats)
                    .padding(.bottom, 4)
            }

            if store.secrets.isEmpty {
                VStack(spacing: 8) {
                    Text("No secrets yet")
                        .font(.title3)
                        .foregroundColor(.secondary)
                    Text("Submit one to begin the game.")
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                List(store.secrets.sorted(by: { $0.createdAt > $1.createdAt })) { secret in
                    SecretRow(secret: secret)
                }
            }
        }
        .padding()
    }
}

struct SecretRow: View {
    let secret: Secret

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    Circle()
                        .fill(secret.isTruth ? Color.green : Color.red)
                        .frame(width: 8, height: 8)
                    Text(secret.value)
                        .font(.body.monospaced())
                }

                HStack(spacing: 8) {
                    Text("by \(secret.submittedBy)")
                        .font(.caption)
                        .foregroundColor(.secondary)

                    if let via = secret.exposedVia {
                        Text("exposed via \(via)")
                            .font(.caption)
                            .foregroundColor(.red.opacity(0.8))
                    }
                }
            }

            Spacer()

            Text(secret.state.uppercased())
                .font(.caption.bold().monospaced())
                .foregroundColor(secret.isTruth ? .green : .red)
                .padding(.horizontal, 8)
                .padding(.vertical, 2)
                .background(
                    (secret.isTruth ? Color.green : Color.red).opacity(0.1)
                )
                .cornerRadius(4)
        }
        .padding(.vertical, 4)
    }
}
