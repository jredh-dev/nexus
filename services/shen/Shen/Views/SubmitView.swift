import SwiftUI

struct SubmitView: View {
    @EnvironmentObject var store: SecretsStore
    @State private var secretValue = ""
    @State private var identity = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Submit a Secret")
                .font(.title.bold())

            TextField("Your identity (or leave blank for anonymous)", text: $identity)
                .textFieldStyle(.roundedBorder)

            HStack {
                TextField("Enter a secret...", text: $secretValue)
                    .textFieldStyle(.roundedBorder)
                    .onSubmit { submit() }

                Button(action: submit) {
                    if store.isLoading {
                        ProgressView()
                            .controlSize(.small)
                    } else {
                        Text("Submit")
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(secretValue.isEmpty || store.isLoading)
                .keyboardShortcut(.return, modifiers: .command)
            }

            if let response = store.lastResponse {
                ResponseCard(response: response)
            }

            if let err = store.errorMessage {
                Text(err)
                    .foregroundColor(.red)
                    .font(.caption)
            }

            Spacer()
        }
        .padding()
    }

    private func submit() {
        let val = secretValue
        let who = identity.isEmpty ? "anonymous" : identity
        secretValue = ""
        Task {
            await store.submit(value: val, as: who)
        }
    }
}

struct ResponseCard: View {
    let response: SubmitResponse

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                if response.selfBetrayal == true {
                    Image(systemName: "arrow.uturn.backward.circle.fill")
                        .foregroundColor(.orange)
                    Text("Self-betrayal!")
                        .font(.headline)
                        .foregroundColor(.orange)
                } else if response.wasNew {
                    Image(systemName: "checkmark.circle.fill")
                        .foregroundColor(.green)
                    Text("New truth recorded")
                        .font(.headline)
                        .foregroundColor(.green)
                } else {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundColor(.red)
                    Text("A secret exposed!")
                        .font(.headline)
                        .foregroundColor(.red)
                }
            }

            Text(response.message)
                .font(.body)

            if let via = response.secret.exposedVia {
                Text("Exposed via: \(via)")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }
        }
        .padding()
        .background(Color.secondary.opacity(0.08))
        .cornerRadius(8)
    }
}
