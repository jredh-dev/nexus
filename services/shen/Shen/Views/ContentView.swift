import SwiftUI

struct ContentView: View {
    @EnvironmentObject var store: SecretsStore
    @State private var selectedTab = 0

    var body: some View {
        TabView(selection: $selectedTab) {
            RiddleView()
                .tabItem { Label("Riddle", systemImage: "questionmark.circle") }
                .tag(0)

            SubmitView()
                .tabItem { Label("Submit", systemImage: "square.and.pencil") }
                .tag(1)

            SecretsListView()
                .tabItem { Label("Secrets", systemImage: "list.bullet") }
                .tag(2)
        }
        .padding()
        .task {
            await store.loadRiddle()
            await store.loadSecrets()
        }
    }
}
