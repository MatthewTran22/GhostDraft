import Link from "next/link";
import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Privacy Policy - GhostDraft",
  description: "GhostDraft privacy policy. Learn about our data practices and how we protect your privacy.",
};

export default function PrivacyPolicy() {
  return (
    <div className="max-w-4xl mx-auto px-6 py-16">
      <h1 className="text-4xl md:text-5xl font-display font-bold text-[var(--pale-gold)] mb-4 text-glow">
        Privacy Policy
      </h1>
      <p className="text-[var(--text-muted)] mb-8">Last updated: January 2, 2026</p>

      <div className="space-y-12">
        <section>
          <h2 className="text-2xl font-display font-semibold text-[var(--pale-gold)] mb-4">Overview</h2>
          <p className="text-[var(--text-secondary)] leading-relaxed">
            GhostDraft is a free, open-source champion select assistant for League of Legends.
            We are committed to protecting your privacy and being transparent about our data practices.
            This policy explains what data we collect, how we use it, and your rights.
          </p>
        </section>

        <section>
          <h2 className="text-2xl font-display font-semibold text-[var(--pale-gold)] mb-4">Data We Collect</h2>

          <h3 className="text-xl font-display font-medium text-[var(--arcane-cyan)] mb-3">Data We Do NOT Collect</h3>
          <ul className="list-disc list-inside text-[var(--text-secondary)] space-y-2 mb-6 ml-4">
            <li>Your summoner name or Riot ID</li>
            <li>Your match history or game statistics</li>
            <li>Your IP address or location</li>
            <li>Your computer hardware information</li>
            <li>Any personal identification information</li>
            <li>Login credentials or account information</li>
          </ul>

          <h3 className="text-xl font-display font-medium text-[var(--arcane-cyan)] mb-3">Data We Access Locally</h3>
          <p className="text-[var(--text-secondary)] mb-4 leading-relaxed">
            GhostDraft connects to the League of Legends client running on your computer using
            the official League Client Update (LCU) API. This connection is:
          </p>
          <ul className="list-disc list-inside text-[var(--text-secondary)] space-y-2 mb-4 ml-4">
            <li>Local only - no data is sent to our servers</li>
            <li>Read-only - we cannot modify your client or account</li>
            <li>Ephemeral - data is only held in memory while the app is running, never saved to disk</li>
          </ul>
          <p className="text-[var(--text-secondary)] leading-relaxed">
            Information accessed includes: current champion select state, selected champions,
            and your recent match history (for personal stats display). This data stays on your
            computer and is never transmitted externally.
          </p>
        </section>

        <section>
          <h2 className="text-2xl font-display font-semibold text-[var(--pale-gold)] mb-4">Aggregated Statistics Data</h2>
          <p className="text-[var(--text-secondary)] mb-4 leading-relaxed">
            GhostDraft downloads aggregated, anonymous match statistics to provide recommendations.
            This data:
          </p>
          <ul className="list-disc list-inside text-[var(--text-secondary)] space-y-2 ml-4">
            <li>Is collected from public match data via the official Riot Games API</li>
            <li>Contains no personally identifiable information</li>
            <li>Consists only of aggregate statistics (win rates, pick rates, item builds)</li>
            <li>Is stored locally on your computer for offline access</li>
            <li>Is updated periodically when new patches are released</li>
          </ul>
        </section>

        <section>
          <h2 className="text-2xl font-display font-semibold text-[var(--pale-gold)] mb-4">Third-Party Services</h2>
          <p className="text-[var(--text-secondary)] mb-4 leading-relaxed">
            GhostDraft uses the following third-party services:
          </p>
          <ul className="list-disc list-inside text-[var(--text-secondary)] space-y-2 ml-4">
            <li>
              <strong className="text-[var(--text-primary)]">Riot Games API</strong> - We use the official Riot Games API
              to collect public match data for generating aggregate statistics. This is done in compliance
              with Riot&apos;s API Terms of Service.
            </li>
            <li>
              <strong className="text-[var(--text-primary)]">Data Dragon</strong> - We use Riot&apos;s Data Dragon service
              to display champion icons, item icons, and game data.
            </li>
          </ul>
        </section>

        <section>
          <h2 className="text-2xl font-display font-semibold text-[var(--pale-gold)] mb-4">Data Storage</h2>
          <p className="text-[var(--text-secondary)] mb-4 leading-relaxed">
            All data used by GhostDraft is stored locally on your computer:
          </p>
          <ul className="list-disc list-inside text-[var(--text-secondary)] space-y-2 ml-4">
            <li>Statistics database: Stored in your user configuration directory</li>
            <li>Champion and item data: Cached locally from Data Dragon</li>
            <li>No cloud storage or remote databases are used for your personal data</li>
          </ul>
        </section>

        <section>
          <h2 className="text-2xl font-display font-semibold text-[var(--pale-gold)] mb-4">Open Source</h2>
          <p className="text-[var(--text-secondary)] mb-4 leading-relaxed">
            GhostDraft is fully open source. You can review our code to verify our privacy practices:
          </p>
          <Link
            href="https://github.com/MatthewTran22/LoLOverlay"
            target="_blank"
            rel="noopener noreferrer"
            className="text-[var(--hextech-gold)] hover:text-[var(--bright-gold)] transition-colors hover-line"
          >
            github.com/MatthewTran22/LoLOverlay
          </Link>
        </section>

        <section>
          <h2 className="text-2xl font-display font-semibold text-[var(--pale-gold)] mb-4">Children&apos;s Privacy</h2>
          <p className="text-[var(--text-secondary)] leading-relaxed">
            GhostDraft does not knowingly collect any personal information from children.
            Since we do not collect personal information from any users, there is no risk
            of collecting data from minors.
          </p>
        </section>

        <section>
          <h2 className="text-2xl font-display font-semibold text-[var(--pale-gold)] mb-4">Changes to This Policy</h2>
          <p className="text-[var(--text-secondary)] leading-relaxed">
            We may update this privacy policy from time to time. Any changes will be posted
            on this page with an updated revision date. We encourage you to review this
            policy periodically.
          </p>
        </section>

        <section>
          <h2 className="text-2xl font-display font-semibold text-[var(--pale-gold)] mb-4">Contact</h2>
          <p className="text-[var(--text-secondary)] leading-relaxed">
            If you have questions about this privacy policy or GhostDraft&apos;s data practices,
            please open an issue on our{" "}
            <Link
              href="https://github.com/MatthewTran22/LoLOverlay/issues"
              target="_blank"
              rel="noopener noreferrer"
              className="text-[var(--hextech-gold)] hover:text-[var(--bright-gold)] transition-colors hover-line"
            >
              GitHub repository
            </Link>
            .
          </p>
        </section>

        <section className="hex-card rounded-xl p-6 mt-12">
          <h2 className="text-xl font-display font-semibold text-[var(--pale-gold)] mb-4">Riot Games Legal</h2>
          <p className="text-[var(--text-muted)] text-sm leading-relaxed">
            GhostDraft is not endorsed by Riot Games and does not reflect the views or opinions
            of Riot Games or anyone officially involved in producing or managing League of Legends.
            League of Legends and Riot Games are trademarks or registered trademarks of Riot Games, Inc.
          </p>
        </section>
      </div>
    </div>
  );
}
