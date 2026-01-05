import './style.css';
import { GetConnectionStatus, GetMetaChampions, GetPersonalStats, GetChampionDetails, GetChampionBuild } from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

// Initial HTML structure
document.querySelector('#app').innerHTML = `
    <div class="overlay-box" id="overlay-box">
        <div class="header drag-region">
            <h1>GhostDraft</h1>
        </div>

        <div class="status-card" id="status-card">
            <div class="status-indicator">
                <div class="status-dot waiting" id="status-dot"></div>
                <span class="status-message" id="status-message">Initializing...</span>
            </div>
        </div>

        <div class="tabs-container hidden" id="tabs-container">
            <div class="tabs-header">
                <button class="tab-btn" data-tab="stats">Stats</button>
                <button class="tab-btn active" data-tab="matchup">Matchup</button>
                <button class="tab-btn" data-tab="build">Build</button>
                <button class="tab-btn" data-tab="teamcomp">Team Comp</button>
                <button class="tab-btn" data-tab="meta">Meta</button>
            </div>

            <div class="tab-content" id="tab-stats">
                <div class="stats-header">Recent Performance</div>
                <div class="stats-content" id="stats-content">
                    <div class="stats-loading">Loading stats...</div>
                </div>
            </div>

            <div class="tab-content active" id="tab-matchup">
                <div class="teamcomp-card hidden" id="teamcomp-card">
                    <div class="teamcomp-warning" id="teamcomp-warning"></div>
                </div>

                <div class="bans-card" id="bans-card">
                    <div class="bans-header">Recommended Bans</div>
                    <div class="bans-subheader" id="bans-subheader"></div>
                    <div class="bans-list" id="bans-list"></div>
                </div>

                <div class="build-card hidden" id="build-card">
                    <div class="build-role" id="build-role"></div>
                    <div class="build-matchup">
                        <span class="winrate-label" id="winrate-label">Win Rate</span>
                        <span class="build-winrate" id="build-winrate"></span>
                    </div>
                </div>
            </div>

            <div class="tab-content" id="tab-build">
                <div class="build-subtabs" id="build-subtabs"></div>
                <div class="build-content" id="build-content"></div>
            </div>

            <div class="tab-content" id="tab-teamcomp">
                <div class="comp-waiting" id="comp-waiting">Waiting for all players to lock in...</div>
                <div class="comp-analysis hidden" id="comp-analysis">
                    <div class="comp-section">
                        <div class="comp-section-header ally">Your Team</div>
                        <div class="comp-archetype" id="ally-archetype"></div>
                        <div class="comp-tags" id="ally-tags"></div>
                        <div class="comp-damage" id="ally-damage"></div>
                    </div>
                    <div class="comp-section">
                        <div class="comp-section-header enemy">Enemy Team</div>
                        <div class="comp-archetype" id="enemy-archetype"></div>
                        <div class="comp-tags" id="enemy-tags"></div>
                        <div class="comp-damage" id="enemy-damage"></div>
                    </div>
                </div>
            </div>

            <div class="tab-content" id="tab-meta">
                <div class="meta-header" id="meta-header">Top Champions by Win Rate</div>
                <div class="meta-content" id="meta-content">
                    <div class="meta-loading">Loading meta data...</div>
                </div>
            </div>
        </div>
    </div>
`;

// DOM elements
const statusDot = document.getElementById('status-dot');
const statusMessage = document.getElementById('status-message');
const statusCard = document.getElementById('status-card');
const tabsContainer = document.getElementById('tabs-container');
const teamcompCard = document.getElementById('teamcomp-card');
const teamcompWarning = document.getElementById('teamcomp-warning');
const bansCard = document.getElementById('bans-card');
const bansSubheader = document.getElementById('bans-subheader');
const bansList = document.getElementById('bans-list');
const buildCard = document.getElementById('build-card');
const buildRole = document.getElementById('build-role');
const buildWinrate = document.getElementById('build-winrate');
const winrateLabel = document.getElementById('winrate-label');
const compWaiting = document.getElementById('comp-waiting');
const compAnalysis = document.getElementById('comp-analysis');
const buildSubtabs = document.getElementById('build-subtabs');
const buildContent = document.getElementById('build-content');
const allyArchetype = document.getElementById('ally-archetype');
const allyTags = document.getElementById('ally-tags');
const allyDamage = document.getElementById('ally-damage');
const enemyArchetype = document.getElementById('enemy-archetype');
const enemyTags = document.getElementById('enemy-tags');
const enemyDamage = document.getElementById('enemy-damage');
const metaHeader = document.getElementById('meta-header');
const metaContent = document.getElementById('meta-content');
const statsContent = document.getElementById('stats-content');

// Tab switching
document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', () => {
        // Update active button
        document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        // Update active content
        document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
        document.getElementById(`tab-${btn.dataset.tab}`).classList.add('active');

        // Load data when specific tabs are clicked
        if (btn.dataset.tab === 'meta') {
            loadMetaData();
        } else if (btn.dataset.tab === 'stats') {
            loadPersonalStats();
        }
    });
});

// State tracking
let metaDataLoaded = false;
let metaRetryCount = 0;
let currentMetaData = null;
let currentMetaRole = 'top';
let isInChampSelect = false;
let statsLoaded = false;
let statsRetryCount = 0;
let selectedChampion = null; // { championId, role }

// Tabs that are only visible during champ select
const champSelectOnlyTabs = ['matchup', 'build', 'teamcomp'];

// Update tab visibility based on champ select state
function updateTabVisibility(inChampSelect) {
    const tabButtons = document.querySelectorAll('.tab-btn');
    const activeTab = document.querySelector('.tab-btn.active');

    tabButtons.forEach(btn => {
        const tabName = btn.dataset.tab;
        if (champSelectOnlyTabs.includes(tabName)) {
            btn.classList.toggle('hidden', !inChampSelect);
        }
    });

    // If leaving champ select and current tab is a champ-select-only tab, switch to Stats
    if (!inChampSelect && activeTab && champSelectOnlyTabs.includes(activeTab.dataset.tab)) {
        const statsBtn = document.querySelector('.tab-btn[data-tab="stats"]');
        if (statsBtn) {
            statsBtn.click();
        }
    }
}

const roleOrder = ['top', 'jungle', 'middle', 'bottom', 'utility'];
const roleNames = {
    'top': 'Top',
    'jungle': 'Jungle',
    'middle': 'Mid',
    'bottom': 'ADC',
    'utility': 'Support'
};

// Render meta role tabs
function renderMetaRoleTabs() {
    return `
        <div class="meta-role-tabs">
            ${roleOrder.map(role => `
                <button class="meta-role-tab ${role === currentMetaRole ? 'active' : ''}" data-role="${role}">
                    ${roleNames[role]}
                </button>
            `).join('')}
        </div>
    `;
}

// Render champions for a specific role
function renderMetaRoleContent(role) {
    if (!currentMetaData || !currentMetaData.roles[role]) {
        return '<div class="meta-empty">No data for this role</div>';
    }

    const champs = currentMetaData.roles[role];
    if (champs.length === 0) {
        return '<div class="meta-empty">No champions found</div>';
    }

    return `
        <div class="meta-role-list">
            <div class="meta-header-row">
                <span class="meta-rank-header">#</span>
                <span class="meta-champ-header">Champion</span>
                <span class="meta-pr-header">Pick %</span>
                <span class="meta-wr-header">Win %</span>
            </div>
            ${champs.map((c, idx) => `
                <div class="meta-champ-row clickable" data-champ-id="${c.championId}" data-role="${role}">
                    <span class="meta-rank">${idx + 1}</span>
                    <img class="meta-icon" src="${c.iconURL}" alt="${c.championName}" />
                    <span class="meta-name">${c.championName}</span>
                    <span class="meta-pr">${c.pickRate.toFixed(1)}%</span>
                    <span class="meta-wr winning">${c.winRate.toFixed(1)}%</span>
                </div>
            `).join('')}
        </div>
    `;
}

// Setup meta role tab click handlers
function setupMetaRoleTabHandlers() {
    document.querySelectorAll('.meta-role-tab').forEach(tab => {
        tab.addEventListener('click', () => {
            currentMetaRole = tab.dataset.role;
            selectedChampion = null; // Reset selection when changing roles
            // Update active state
            document.querySelectorAll('.meta-role-tab').forEach(t => t.classList.remove('active'));
            tab.classList.add('active');
            // Update content
            document.getElementById('meta-role-content').innerHTML = renderMetaRoleContent(currentMetaRole);
            setupMetaChampionClickHandlers();
        });
    });
}

// Setup click handlers for champion rows
function setupMetaChampionClickHandlers() {
    document.querySelectorAll('.meta-champ-row.clickable').forEach(row => {
        row.addEventListener('click', () => {
            const champId = parseInt(row.dataset.champId);
            const role = row.dataset.role;

            selectedChampion = { championId: champId, role };

            // Hide the tier list view and show details view
            document.getElementById('meta-tier-list').classList.add('hidden');
            document.getElementById('meta-details-view').classList.remove('hidden');

            // Load champion details
            loadChampionDetails(champId, role);
        });
    });
}

// Go back to tier list from champion details
function showMetaTierList() {
    selectedChampion = null;
    document.getElementById('meta-tier-list').classList.remove('hidden');
    document.getElementById('meta-details-view').classList.add('hidden');
}

// Helper to render basic items (no win rate) - shared between Build tab and Meta details
function renderBasicItems(items) {
    if (items && items.length > 0) {
        return items.map(item => `
            <div class="item-slot">
                <img class="item-icon" src="${item.iconURL}" alt="${item.name}" title="${item.name}" />
            </div>
        `).join('');
    }
    return '<div class="items-empty">No data</div>';
}

// Helper to render items with win rate - shared between Build tab and Meta details
function renderItemsWithWR(items) {
    if (items && items.length > 0) {
        return items.map(item => {
            const wr = item.winRate ? item.winRate.toFixed(1) : '?';
            const wrClass = item.winRate >= 51 ? 'winning' : item.winRate <= 49 ? 'losing' : 'even';
            return `
                <div class="item-slot-wr">
                    <img class="item-icon" src="${item.iconURL}" alt="${item.name}" title="${item.name} (${item.games} games)" />
                    <span class="item-wr ${wrClass}">${wr}%</span>
                </div>
            `;
        }).join('');
    }
    return '<div class="items-empty">No data</div>';
}

// Load and display champion details
function loadChampionDetails(championId, role) {
    const detailsEl = document.getElementById('meta-champion-details');
    detailsEl.innerHTML = '<div class="details-loading">Loading details...</div>';

    // Fetch both matchup data and build data in parallel
    Promise.all([
        GetChampionDetails(championId, role),
        GetChampionBuild(championId, role)
    ]).then(([matchupData, buildData]) => {
        if (!matchupData.hasData && !buildData.hasItems) {
            detailsEl.innerHTML = `
                <div class="details-header">
                    <button class="details-back-btn" onclick="showMetaTierList()">←</button>
                    <div class="details-header-text">
                        <span class="details-champ-name">No Data</span>
                    </div>
                </div>
                <div class="details-empty">No detailed data available</div>
            `;
            return;
        }

        const champName = matchupData.championName || buildData.championName;
        const splashURL = buildData.splashURL || '';

        let html = `
            <div class="details-banner" style="background-image: url('${splashURL}');">
                <div class="details-banner-overlay"></div>
                <div class="details-banner-content">
                    <div class="details-banner-header">
                        <button class="details-back-btn" onclick="showMetaTierList()">←</button>
                        <div class="details-banner-info">
                            <span class="details-champ-name">${champName}</span>
                            <span class="details-role">${formatRole(role)}</span>
                        </div>
                    </div>
                    <div class="details-banner-build">
                        <div class="build-subtabs hidden" id="details-build-subtabs"></div>
                        <div class="build-content" id="details-build-content"></div>
                    </div>
                </div>
            </div>
        `;

        // Counters row - show your WR against them (low = they counter you)
        const counters = matchupData.counters || [];
        html += `
            <div class="details-counters-row">
                <span class="details-counters-label">Counters</span>
                <div class="details-counters-list">
                    ${counters.length > 0
                        ? counters.slice(0, 6).map(m => `
                            <div class="details-counter">
                                <img class="details-counter-icon" src="${m.iconURL}" alt="${m.championName}" title="${m.championName}" />
                                <span class="details-counter-wr">${m.winRate.toFixed(0)}%</span>
                                <span class="details-counter-games">${m.games} games</span>
                            </div>
                        `).join('')
                        : '<span class="details-counters-empty">Not enough data</span>'
                    }
                </div>
            </div>
        `;

        detailsEl.innerHTML = html;

        // Use the shared renderBuildsToContainer function - exact same as Build tab
        if (buildData.hasItems && buildData.builds && buildData.builds.length > 0) {
            const subtabsEl = document.getElementById('details-build-subtabs');
            const contentEl = document.getElementById('details-build-content');
            renderBuildsToContainer(subtabsEl, contentEl, buildData.builds);
        }
    }).catch(err => {
        console.error('Failed to load champion details:', err);
        detailsEl.innerHTML = `
            <div class="details-header">
                <button class="details-back-btn" onclick="showMetaTierList()">←</button>
                <div class="details-header-text">
                    <span class="details-champ-name">Error</span>
                </div>
            </div>
            <div class="details-empty">Failed to load details</div>
        `;
    });
}

// Make showMetaTierList available globally for onclick
window.showMetaTierList = showMetaTierList;

// Load and display meta champions
function loadMetaData() {
    if (metaDataLoaded) {
        // If already loaded, just make sure we're showing tier list (not details)
        showMetaTierList();
        return;
    }

    console.log('Loading meta data...');
    GetMetaChampions()
        .then(data => {
            console.log('Meta data received:', data);
            if (!data.hasData) {
                // Retry a few times in case stats are still loading
                if (metaRetryCount < 5) {
                    metaRetryCount++;
                    metaContent.innerHTML = `<div class="meta-loading">Loading stats... (attempt ${metaRetryCount})</div>`;
                    setTimeout(loadMetaData, 2000);
                    return;
                }
                metaContent.innerHTML = '<div class="meta-empty">No meta data available. Make sure stats are downloaded.</div>';
                return;
            }

            metaHeader.textContent = `Top Champions - Patch ${data.patch}`;
            metaDataLoaded = true;
            currentMetaData = data;

            metaContent.innerHTML = `
                <div id="meta-tier-list">
                    ${renderMetaRoleTabs()}
                    <div id="meta-role-content">
                        ${renderMetaRoleContent(currentMetaRole)}
                    </div>
                </div>
                <div id="meta-details-view" class="hidden">
                    <div id="meta-champion-details"></div>
                </div>
            `;

            setupMetaRoleTabHandlers();
            setupMetaChampionClickHandlers();
        })
        .catch(err => {
            console.error('Failed to load meta data:', err);
            metaContent.innerHTML = '<div class="meta-empty">Failed to load meta data</div>';
        });
}

// Load and display personal stats
function loadPersonalStats() {
    // Always refresh stats when tab is clicked (don't cache)
    statsContent.innerHTML = '<div class="stats-loading">Loading your stats...</div>';

    GetPersonalStats()
        .then(data => {
            if (!data.hasData) {
                if (statsRetryCount < 3) {
                    statsRetryCount++;
                    statsContent.innerHTML = `<div class="stats-loading">Waiting for League Client... (attempt ${statsRetryCount})</div>`;
                    setTimeout(loadPersonalStats, 2000);
                    return;
                }
                statsContent.innerHTML = '<div class="stats-empty">No match history available. Make sure League Client is running.</div>';
                return;
            }

            statsRetryCount = 0;
            statsLoaded = true;

            const wrClass = data.winRate >= 55 ? 'winning' : data.winRate <= 45 ? 'losing' : 'even';
            const kdaClass = data.avgKDA >= 3 ? 'excellent' : data.avgKDA >= 2 ? 'good' : 'average';

            // Overall ranked stats strip at top
            let html = `
                <div class="stats-overall-strip">
                    <div class="stats-strip-item">
                        <span class="stats-strip-value">${data.wins}W ${data.losses}L</span>
                        <span class="stats-strip-label">Record</span>
                    </div>
                    <div class="stats-strip-item">
                        <span class="stats-strip-value ${wrClass}">${data.winRate.toFixed(0)}%</span>
                        <span class="stats-strip-label">Win Rate</span>
                    </div>
                    <div class="stats-strip-item">
                        <span class="stats-strip-value ${kdaClass}">${data.avgKDA.toFixed(2)}</span>
                        <span class="stats-strip-label">KDA</span>
                    </div>
                    <div class="stats-strip-item">
                        <span class="stats-strip-value">${data.avgCSPerMin.toFixed(1)}</span>
                        <span class="stats-strip-label">CS/min</span>
                    </div>
                </div>
            `;

            // Champion banner with splash art background
            if (data.championStats && data.championStats.length > 0) {
                const topChamp = data.championStats[0];
                const topWrClass = topChamp.winRate >= 55 ? 'winning' : topChamp.winRate <= 45 ? 'losing' : 'even';
                const topKdaClass = topChamp.avgKDA >= 3 ? 'excellent' : topChamp.avgKDA >= 2 ? 'good' : 'average';

                html += `
                    <div class="stats-banner" style="background-image: url('${topChamp.splashURL}');">
                        <div class="stats-banner-overlay"></div>
                        <div class="stats-banner-content">
                            <div class="stats-banner-label">Most Played in Ranked</div>
                            <div class="stats-banner-name-row">
                                <img class="stats-banner-role-icon" src="${topChamp.roleIconURL}" alt="${topChamp.role}" />
                                <span class="stats-banner-name">${topChamp.championName}</span>
                            </div>
                            <div class="stats-banner-role">${topChamp.role}</div>
                            <div class="stats-banner-games">${topChamp.games} games</div>
                            <div class="stats-banner-stats">
                                <div class="stats-banner-stat">
                                    <span class="stat-value ${topWrClass}">${topChamp.winRate.toFixed(0)}%</span>
                                    <span class="stat-label">Win Rate</span>
                                </div>
                                <div class="stats-banner-stat">
                                    <span class="stat-value ${topKdaClass}">${topChamp.avgKDA.toFixed(2)}</span>
                                    <span class="stat-label">KDA</span>
                                </div>
                                <div class="stats-banner-stat">
                                    <span class="stat-value">${topChamp.avgCSPerMin.toFixed(1)}</span>
                                    <span class="stat-label">CS/min</span>
                                </div>
                            </div>
                        </div>
                    </div>
                `;

                // Other recently played champions
                if (data.championStats.length > 1) {
                    html += `
                        <div class="stats-other-champs">
                            <div class="stats-other-header">Also Played</div>
                            <div class="stats-other-list">
                                ${data.championStats.slice(1).map(c => {
                                    const cWrClass = c.winRate >= 55 ? 'winning' : c.winRate <= 45 ? 'losing' : 'even';
                                    return `
                                        <div class="stats-other-row">
                                            <img class="stats-other-icon" src="${c.iconURL}" alt="${c.championName}" />
                                            <span class="stats-other-name">${c.championName}</span>
                                            <span class="stats-other-games">${c.games}</span>
                                            <span class="stats-other-wr ${cWrClass}">${c.winRate.toFixed(0)}%</span>
                                        </div>
                                    `;
                                }).join('')}
                            </div>
                        </div>
                    `;
                }
            }

            statsContent.innerHTML = html;
        })
        .catch(err => {
            console.error('Failed to load personal stats:', err);
            statsContent.innerHTML = '<div class="stats-empty">Failed to load stats</div>';
        });
}

// Format role name for display
function formatRole(role) {
    const roleMap = {
        'utility': 'Support',
        'middle': 'Mid',
        'bottom': 'ADC',
        'jungle': 'Jungle',
        'top': 'Top'
    };
    return roleMap[role] || role || '';
}

// Update connection status
function updateStatus(status) {
    statusMessage.textContent = status.message;
    statusDot.className = status.connected ? 'status-dot connected' : 'status-dot waiting';
}

// Update champ select state
function updateChampSelect(data) {
    // Always show tabs container - Meta tab works outside champ select too
    statusCard.classList.add('hidden');
    tabsContainer.classList.remove('hidden');

    // Track champ select state and update tab visibility
    const wasInChampSelect = isInChampSelect;
    isInChampSelect = data.inChampSelect;

    // Only update visibility when state changes
    if (wasInChampSelect !== isInChampSelect) {
        updateTabVisibility(isInChampSelect);
    }

    if (!data.inChampSelect) {
        // Reset team comp UI when leaving champ select
        compWaiting.classList.remove('hidden');
        compAnalysis.classList.add('hidden');
        teamcompCard.classList.add('hidden');
        return;
    }
}

// Update team comp warning
function updateTeamComp(data) {
    if (!data || !data.show) {
        teamcompCard.classList.add('hidden');
        return;
    }

    teamcompCard.classList.remove('hidden');
    teamcompCard.className = `teamcomp-card ${data.severity}`;
    teamcompWarning.textContent = data.recommendation;
}

// Update recommended bans
function updateBans(data) {
    if (!data || !data.hasBans) {
        bansSubheader.textContent = '';
        bansList.innerHTML = '';
        return;
    }

    // No matchup data for this pick
    if (data.noData || !data.bans || data.bans.length === 0) {
        bansSubheader.textContent = `${data.championName} ${formatRole(data.role)}`;
        bansList.innerHTML = `<div class="no-data-msg">WTF kind of pick is this?</div>`;
        return;
    }

    // Update header
    bansSubheader.textContent = `Counters for ${data.championName} (${formatRole(data.role)})`;

    // Build ban list HTML
    let html = '';
    for (let i = 0; i < data.bans.length; i++) {
        const ban = data.bans[i];
        const wr = typeof ban.winRate === 'number' ? ban.winRate.toFixed(1) : ban.winRate;
        const dmgClass = ban.damageType === 'AP' ? 'ap' : ban.damageType === 'AD' ? 'ad' : 'mixed';
        html += `
            <div class="ban-row">
                <span class="ban-rank">${i + 1}</span>
                <img class="ban-icon" src="${ban.iconURL}" alt="${ban.championName}" />
                <span class="ban-name">${ban.championName}</span>
                <span class="ban-dmg ${dmgClass}">${ban.damageType}</span>
                <span class="ban-wr losing">${wr}%</span>
            </div>
        `;
    }
    bansList.innerHTML = html;
}

// Update build/matchup data
function updateBuild(data) {
    if (!data.hasBuild) {
        buildCard.classList.add('hidden');
        return;
    }

    buildCard.classList.remove('hidden');

    buildRole.textContent = formatRole(data.role);
    winrateLabel.textContent = data.winRateLabel || 'Win Rate';
    buildWinrate.textContent = data.winRate;

    buildWinrate.classList.remove('winning', 'losing', 'even');
    if (data.matchupStatus) {
        buildWinrate.classList.add(data.matchupStatus);
    }
}

// Update full team comp analysis (when all locked in)
function updateFullComp(data) {
    if (!data || !data.ready) {
        compWaiting.classList.remove('hidden');
        compAnalysis.classList.add('hidden');
        return;
    }

    compWaiting.classList.add('hidden');
    compAnalysis.classList.remove('hidden');

    // Render ally team
    allyArchetype.textContent = data.allyArchetype;
    allyTags.innerHTML = (data.allyTags || []).map(tag =>
        `<span class="comp-tag">${tag}</span>`
    ).join('') || '<span class="comp-tag-none">No dominant tags</span>';
    allyDamage.innerHTML = `
        <span class="dmg-bar">
            <span class="dmg-ap" style="width: ${data.allyAP}%">${data.allyAP}% AP</span>
            <span class="dmg-ad" style="width: ${data.allyAD}%">${data.allyAD}% AD</span>
        </span>
    `;

    // Render enemy team
    enemyArchetype.textContent = data.enemyArchetype;
    enemyTags.innerHTML = (data.enemyTags || []).map(tag =>
        `<span class="comp-tag">${tag}</span>`
    ).join('') || '<span class="comp-tag-none">No dominant tags</span>';
    enemyDamage.innerHTML = `
        <span class="dmg-bar">
            <span class="dmg-ap" style="width: ${data.enemyAP}%">${data.enemyAP}% AP</span>
            <span class="dmg-ad" style="width: ${data.enemyAD}%">${data.enemyAD}% AD</span>
        </span>
    `;
}

// Current builds data for sub-tab switching (used by Build tab)
let currentBuildsData = null;

// Shared function to render builds to any container
// This is the single source of truth for build rendering - used by both Build tab and Meta details
function renderBuildsToContainer(subtabsEl, contentEl, builds) {
    // Hide subtabs - we only have one build
    subtabsEl.innerHTML = '';
    subtabsEl.classList.add('hidden');

    if (!builds || builds.length === 0) {
        contentEl.innerHTML = '<div class="items-empty">No build data</div>';
        return;
    }

    const build = builds[0];
    if (!build) {
        contentEl.innerHTML = '<div class="items-empty">No build data</div>';
        return;
    }

    contentEl.innerHTML = `
        <div class="items-section">
            <div class="items-header">Core Items</div>
            <div class="items-grid">${renderBasicItems(build.coreItems)}</div>
        </div>
        <div class="items-section">
            <div class="items-header">4th Item Options</div>
            <div class="items-grid">${renderItemsWithWR(build.fourthItems)}</div>
        </div>
        <div class="items-section">
            <div class="items-header">5th Item Options</div>
            <div class="items-grid">${renderItemsWithWR(build.fifthItems)}</div>
        </div>
        <div class="items-section">
            <div class="items-header">6th Item Options</div>
            <div class="items-grid">${renderItemsWithWR(build.sixthItems)}</div>
        </div>
    `;
}

// Update item build with multiple build paths (Build tab during champ select)
function updateItems(data) {
    if (!data || !data.hasItems || !data.builds || data.builds.length === 0) {
        buildSubtabs.innerHTML = '';
        buildContent.innerHTML = '<div class="items-empty">Select a champion...</div>';
        currentBuildsData = null;
        return;
    }

    currentBuildsData = data.builds;
    renderBuildsToContainer(buildSubtabs, buildContent, data.builds);
}

// Event listeners
EventsOn('lcu:status', updateStatus);
EventsOn('champselect:update', updateChampSelect);
EventsOn('build:update', updateBuild);
EventsOn('bans:update', updateBans);
EventsOn('teamcomp:update', updateTeamComp);
EventsOn('fullcomp:update', updateFullComp);
EventsOn('items:update', updateItems);

// Get initial status
GetConnectionStatus()
    .then(updateStatus)
    .catch(() => {
        updateStatus({ connected: false, message: 'Waiting for League...' });
    });

// Initialize on startup - start with Stats view (not in champ select)
setTimeout(() => {
    tabsContainer.classList.remove('hidden');
    statusCard.classList.add('hidden');

    // Hide champ-select-only tabs initially
    updateTabVisibility(false);

    // Switch to Stats tab and load data
    const statsBtn = document.querySelector('.tab-btn[data-tab="stats"]');
    if (statsBtn) {
        statsBtn.click();
    }
}, 500);
