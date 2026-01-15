import Link from "next/link";

export default function Home() {
  return (
    <div>
      {/* Hero Section */}
      <section className="relative overflow-hidden min-h-[80vh] flex items-center">
        <div className="absolute inset-0 bg-gradient-to-b from-[var(--hextech-gold)]/5 via-transparent to-transparent pointer-events-none" />
        <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_center,_var(--arcane-blue)_0%,_transparent_70%)] pointer-events-none" />

        <div className="max-w-6xl mx-auto px-6 py-24 md:py-32 relative z-10">
          <div className="text-center">
            <h1 className="text-4xl md:text-6xl lg:text-7xl font-display font-bold mb-6 reveal">
              <span className="text-[var(--text-primary)]">Dominate Champion Select with</span>
              <br />
              <span className="gradient-text text-glow">GhostDraft</span>
            </h1>
            <p className="text-xl text-[var(--text-secondary)] max-w-2xl mx-auto mb-10 reveal reveal-delay-1 leading-relaxed">
              Real-time overlay for League of Legends that provides matchup statistics,
              item build recommendations, and team composition analysis during champion select.
            </p>
            <div className="flex flex-col sm:flex-row gap-4 justify-center reveal reveal-delay-2">
              <a
                href="https://github.com/MatthewTran22/LoLOverlay/releases/latest/download/GhostDraft.zip"
                className="btn-hextech text-lg"
                download
              >
                Download for Windows
              </a>
              <Link
                href="https://github.com/MatthewTran22/LoLOverlay"
                target="_blank"
                rel="noopener noreferrer"
                className="btn-outline text-lg"
              >
                View on GitHub
              </Link>
            </div>
          </div>
        </div>
      </section>

      {/* Preview Section */}
      <section className="py-20 bg-[var(--abyss)]">
        <div className="max-w-6xl mx-auto px-6">
          <h2 className="text-3xl md:text-4xl font-display font-bold text-[var(--pale-gold)] text-center mb-4 text-glow">
            See It In Action
          </h2>
          <p className="text-[var(--text-secondary)] text-center max-w-2xl mx-auto mb-12">
            GhostDraft displays a clean overlay during champion select with all the information you need.
          </p>

          <div className="relative mx-auto max-w-4xl float">
            {/* App Preview Mockup */}
            <div className="hex-card rounded-2xl overflow-hidden shadow-2xl">
              <div className="bg-[var(--deep-navy)] px-4 py-3 border-b border-[var(--hextech-gold)]/20 flex items-center gap-2">
                <div className="w-3 h-3 rounded-full bg-red-500/80" />
                <div className="w-3 h-3 rounded-full bg-yellow-500/80" />
                <div className="w-3 h-3 rounded-full bg-green-500/80" />
                <span className="text-[var(--hextech-gold)] text-sm ml-2 font-display">GhostDraft</span>
              </div>
              <div className="p-6 md:p-8 bg-[var(--deep-navy)]">
                {/* Mock UI */}
                <div className="grid grid-cols-4 gap-4 mb-6">
                  {["Matchup", "Build", "Team Comp", "Meta"].map((tab, i) => (
                    <div
                      key={tab}
                      className={`text-center py-2 rounded-lg text-sm font-medium transition-all ${
                        i === 0
                          ? "bg-[var(--hextech-gold)]/20 text-[var(--hextech-gold)] border border-[var(--hextech-gold)]/50"
                          : "text-[var(--text-muted)] border border-[var(--arcane-blue)] hover:border-[var(--hextech-gold)]/30"
                      }`}
                    >
                      {tab}
                    </div>
                  ))}
                </div>

                {/* Mock Matchup Content - Master Yi Example */}
                <div className="space-y-4">
                  <div className="flex items-center justify-between p-4 bg-[var(--arcane-blue)]/50 rounded-lg border border-[var(--hextech-gold)]/20">
                    <div className="flex items-center gap-4">
                      <img
                        src="https://ddragon.leagueoflegends.com/cdn/14.24.1/img/champion/MasterYi.png"
                        alt="Master Yi"
                        className="w-12 h-12 rounded-lg border border-[var(--hextech-gold)]/30"
                      />
                      <div>
                        <div className="text-[var(--text-primary)] font-medium">Master Yi</div>
                        <div className="text-[var(--text-muted)] text-sm">vs Rammus</div>
                      </div>
                    </div>
                    <div className="text-right">
                      <div className="text-2xl font-bold wr-low">43.8%</div>
                      <div className="text-[var(--text-muted)] text-sm">Win Rate</div>
                    </div>
                  </div>

                  <div className="text-[var(--text-secondary)] text-sm font-medium mb-2">Counter Matchups</div>
                  <div className="grid grid-cols-3 gap-3">
                    {[
                      { champ: "Rammus", icon: "Rammus", wr: "43.8%", class: "wr-low" },
                      { champ: "Jax", icon: "Jax", wr: "45.2%", class: "wr-low" },
                      { champ: "Elise", icon: "Elise", wr: "46.1%", class: "wr-low" },
                    ].map((item) => (
                      <div
                        key={item.champ}
                        className="p-3 bg-[var(--arcane-blue)]/30 rounded-lg border border-[var(--hextech-gold)]/10 text-center hover:border-[var(--hextech-gold)]/30 transition-all"
                      >
                        <img
                          src={`https://ddragon.leagueoflegends.com/cdn/14.24.1/img/champion/${item.icon}.png`}
                          alt={item.champ}
                          className="w-10 h-10 rounded-full mx-auto mb-2"
                        />
                        <div className="text-[var(--text-secondary)] text-sm">{item.champ}</div>
                        <div className={`text-sm font-medium ${item.class}`}>{item.wr}</div>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>

            {/* Decorative glow */}
            <div className="absolute -inset-8 bg-[radial-gradient(ellipse_at_center,_var(--hextech-gold)_0%,_transparent_70%)] opacity-10 blur-3xl -z-10" />
          </div>
        </div>
      </section>

      {/* Features Section */}
      <section id="features" className="py-24">
        <div className="max-w-6xl mx-auto px-6">
          <h2 className="text-3xl md:text-4xl font-display font-bold text-[var(--pale-gold)] text-center mb-4 text-glow">
            Everything You Need in Champion Select
          </h2>
          <p className="text-[var(--text-secondary)] text-center max-w-2xl mx-auto mb-16">
            GhostDraft connects to the League Client API to provide real-time insights
            that help you make better decisions.
          </p>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-8">
            <FeatureCard
              title="Matchup Win Rates"
              description="See your win rate against the enemy laner based on aggregated match data. Know if you're in a favorable or unfavorable matchup."
              icon="chart"
              delay={1}
            />
            <FeatureCard
              title="Counter Recommendations"
              description="Get recommended bans based on your champion's hardest counters. Never get caught off guard by a bad matchup."
              icon="shield"
              delay={2}
            />
            <FeatureCard
              title="Item Build Order"
              description="See the most successful item builds for your champion in your role, with win rates for each build path."
              icon="build"
              delay={3}
            />
            <FeatureCard
              title="Team Composition Analysis"
              description="Get warnings when your team is too AD or AP heavy. Know when to pick a different damage type."
              icon="team"
              delay={4}
            />
            <FeatureCard
              title="Meta Champions"
              description="View the top performing champions for each role based on current patch data."
              icon="trophy"
              delay={5}
            />
            <FeatureCard
              title="Automatic Detection"
              description="GhostDraft automatically detects when you enter champion select and displays relevant information."
              icon="bolt"
              delay={6}
            />
          </div>
        </div>
      </section>

      {/* How It Works */}
      <section className="py-24 bg-[var(--abyss)]">
        <div className="max-w-6xl mx-auto px-6">
          <h2 className="text-3xl md:text-4xl font-display font-bold text-[var(--pale-gold)] text-center mb-16 text-glow">
            How It Works
          </h2>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-8 relative">
            {/* Connecting line */}
            <div className="hidden md:block absolute top-8 left-1/6 right-1/6 h-0.5 bg-gradient-to-r from-transparent via-[var(--hextech-gold)]/30 to-transparent" />

            <StepCard
              number="1"
              title="Download & Run"
              description="Download GhostDraft and run it. No installation required - it's a single executable."
            />
            <StepCard
              number="2"
              title="Start League"
              description="Launch League of Legends. GhostDraft automatically connects to the client."
            />
            <StepCard
              number="3"
              title="Enter Champ Select"
              description="Queue up for a game. GhostDraft displays matchup data as you pick your champion."
            />
          </div>
        </div>
      </section>

      {/* Data & Privacy */}
      <section className="py-24">
        <div className="max-w-6xl mx-auto px-6">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-12 items-center">
            <div>
              <h2 className="text-3xl md:text-4xl font-display font-bold text-[var(--pale-gold)] mb-6 text-glow">
                Your Privacy Matters
              </h2>
              <ul className="space-y-4">
                <li className="flex items-start gap-3">
                  <span className="text-[var(--arcane-cyan)] mt-1 text-glow-cyan">&#10003;</span>
                  <span className="text-[var(--text-secondary)]">
                    <strong className="text-[var(--text-primary)]">No account required</strong> - GhostDraft works without any login or registration.
                  </span>
                </li>
                <li className="flex items-start gap-3">
                  <span className="text-[var(--arcane-cyan)] mt-1 text-glow-cyan">&#10003;</span>
                  <span className="text-[var(--text-secondary)]">
                    <strong className="text-[var(--text-primary)]">No personal data collected</strong> - We don&apos;t store your summoner name, match history, or any personal information.
                  </span>
                </li>
                <li className="flex items-start gap-3">
                  <span className="text-[var(--arcane-cyan)] mt-1 text-glow-cyan">&#10003;</span>
                  <span className="text-[var(--text-secondary)]">
                    <strong className="text-[var(--text-primary)]">Open source</strong> - All code is publicly available on GitHub for transparency.
                  </span>
                </li>
                <li className="flex items-start gap-3">
                  <span className="text-[var(--arcane-cyan)] mt-1 text-glow-cyan">&#10003;</span>
                  <span className="text-[var(--text-secondary)]">
                    <strong className="text-[var(--text-primary)]">Local processing</strong> - Data is processed locally on your machine.
                  </span>
                </li>
              </ul>
              <Link
                href="/privacy"
                className="inline-block mt-6 text-[var(--hextech-gold)] hover:text-[var(--bright-gold)] transition-colors hover-line"
              >
                Read our full Privacy Policy &rarr;
              </Link>
            </div>
            <div className="hex-card rounded-2xl p-8">
              <h3 className="text-xl font-display font-semibold text-[var(--pale-gold)] mb-4">Data Sources</h3>
              <p className="text-[var(--text-secondary)] mb-4 leading-relaxed">
                GhostDraft uses aggregated, anonymous match data to provide statistics.
                We collect public match data through the official Riot Games API.
              </p>
              <p className="text-[var(--text-secondary)] leading-relaxed">
                Champion and item information is sourced from Riot&apos;s Data Dragon,
                ensuring accurate and up-to-date game data.
              </p>
            </div>
          </div>
        </div>
      </section>

      {/* Download Section */}
      <section id="download" className="py-24 bg-[var(--abyss)] relative overflow-hidden">
        <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_center,_var(--hextech-gold)_0%,_transparent_70%)] opacity-5" />

        <div className="max-w-6xl mx-auto px-6 text-center relative z-10">
          <h2 className="text-3xl md:text-4xl font-display font-bold text-[var(--pale-gold)] mb-6 text-glow">
            Ready to Improve Your Draft?
          </h2>
          <p className="text-[var(--text-secondary)] max-w-2xl mx-auto mb-10">
            Download GhostDraft for free and start making better decisions in champion select.
          </p>
          <div className="flex flex-col sm:flex-row gap-4 justify-center">
            <a
              href="https://github.com/MatthewTran22/LoLOverlay/releases/latest/download/GhostDraft.zip"
              className="btn-hextech text-lg pulse-glow"
              download
            >
              Download for Windows
            </a>
          </div>
          <p className="text-[var(--text-muted)] text-sm mt-6">
            Windows 10/11 required. macOS and Linux coming soon.
          </p>
        </div>
      </section>
    </div>
  );
}

function FeatureCard({ title, description, icon, delay }: { title: string; description: string; icon: string; delay: number }) {
  const iconSvg = {
    chart: (
      <svg className="w-8 h-8" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" />
      </svg>
    ),
    shield: (
      <svg className="w-8 h-8" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
      </svg>
    ),
    build: (
      <svg className="w-8 h-8" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
      </svg>
    ),
    team: (
      <svg className="w-8 h-8" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0zm6 3a2 2 0 11-4 0 2 2 0 014 0zM7 10a2 2 0 11-4 0 2 2 0 014 0z" />
      </svg>
    ),
    trophy: (
      <svg className="w-8 h-8" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M5 3v4M3 5h4M6 17v4m-2-2h4m5-16l2.286 6.857L21 12l-5.714 2.143L13 21l-2.286-6.857L5 12l5.714-2.143L13 3z" />
      </svg>
    ),
    bolt: (
      <svg className="w-8 h-8" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M13 10V3L4 14h7v7l9-11h-7z" />
      </svg>
    ),
  };

  return (
    <div className={`hex-card rounded-xl p-6 reveal reveal-delay-${delay}`}>
      <div className="w-14 h-14 bg-[var(--hextech-gold)]/10 rounded-lg flex items-center justify-center text-[var(--hextech-gold)] mb-4 border border-[var(--hextech-gold)]/20">
        {iconSvg[icon as keyof typeof iconSvg]}
      </div>
      <h3 className="text-xl font-display font-semibold text-[var(--pale-gold)] mb-2">{title}</h3>
      <p className="text-[var(--text-secondary)] leading-relaxed">{description}</p>
    </div>
  );
}

function StepCard({ number, title, description }: { number: string; title: string; description: string }) {
  return (
    <div className="text-center relative">
      <div className="w-16 h-16 bg-gradient-to-br from-[var(--hextech-gold)] to-[var(--bright-gold)] rounded-full flex items-center justify-center text-[var(--void-black)] font-bold text-2xl mx-auto mb-6 pulse-glow font-display">
        {number}
      </div>
      <h3 className="text-xl font-display font-semibold text-[var(--pale-gold)] mb-3">{title}</h3>
      <p className="text-[var(--text-secondary)] leading-relaxed">{description}</p>
    </div>
  );
}
