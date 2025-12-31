import './style.css';
import { GetConnectionStatus, GetMetaChampions } from '../wailsjs/go/main/App';
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
                <button class="tab-btn active" data-tab="matchup">Matchup</button>
                <button class="tab-btn" data-tab="build">Build</button>
                <button class="tab-btn" data-tab="teamcomp">Team Comp</button>
                <button class="tab-btn" data-tab="meta">Meta</button>
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

// Tab switching
document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', () => {
        // Update active button
        document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        // Update active content
        document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
        document.getElementById(`tab-${btn.dataset.tab}`).classList.add('active');

        // Load meta data when meta tab is clicked
        if (btn.dataset.tab === 'meta') {
            loadMetaData();
        }
    });
});

// Meta data loaded flag
let metaDataLoaded = false;
let metaRetryCount = 0;
let currentMetaData = null;
let currentMetaRole = 'top';

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
                <div class="meta-champ-row">
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
            // Update active state
            document.querySelectorAll('.meta-role-tab').forEach(t => t.classList.remove('active'));
            tab.classList.add('active');
            // Update content
            document.getElementById('meta-role-content').innerHTML = renderMetaRoleContent(currentMetaRole);
        });
    });
}

// Load and display meta champions
function loadMetaData() {
    if (metaDataLoaded) return;

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
                ${renderMetaRoleTabs()}
                <div id="meta-role-content">
                    ${renderMetaRoleContent(currentMetaRole)}
                </div>
            `;

            setupMetaRoleTabHandlers();
        })
        .catch(err => {
            console.error('Failed to load meta data:', err);
            metaContent.innerHTML = '<div class="meta-empty">Failed to load meta data</div>';
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
    // Always show tabs - Meta tab works outside champ select too
    statusCard.classList.add('hidden');
    tabsContainer.classList.remove('hidden');

    if (!data.inChampSelect) {
        // Not in champ select - could show a message or just keep tabs visible
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

// Current builds data for sub-tab switching
let currentBuildsData = null;

// Update item build with multiple build paths
function updateItems(data) {
    if (!data || !data.hasItems || !data.builds || data.builds.length === 0) {
        buildSubtabs.innerHTML = '';
        buildContent.innerHTML = '<div class="items-empty">Select a champion...</div>';
        currentBuildsData = null;
        return;
    }

    currentBuildsData = data.builds;

    // Render sub-tabs
    buildSubtabs.innerHTML = data.builds.map((build, idx) => {
        const wr = build.winRate ? build.winRate.toFixed(1) : '?';
        const wrClass = build.winRate >= 51 ? 'winning' : build.winRate <= 49 ? 'losing' : 'even';
        return `
            <button class="build-subtab ${idx === 0 ? 'active' : ''}" data-build-idx="${idx}">
                <span class="subtab-name">${build.name}</span>
                <span class="subtab-wr ${wrClass}">${wr}%</span>
                <span class="subtab-games">${build.games} games</span>
            </button>
        `;
    }).join('');

    // Add click handlers for sub-tabs
    buildSubtabs.querySelectorAll('.build-subtab').forEach(btn => {
        btn.addEventListener('click', () => {
            buildSubtabs.querySelectorAll('.build-subtab').forEach(b => b.classList.remove('active'));
            btn.classList.add('active');
            renderBuildContent(parseInt(btn.dataset.buildIdx));
        });
    });

    // Render first build by default
    renderBuildContent(0);
}

// Render content for a specific build path
function renderBuildContent(buildIdx) {
    if (!currentBuildsData || !currentBuildsData[buildIdx]) {
        buildContent.innerHTML = '<div class="items-empty">No build data</div>';
        return;
    }

    const build = currentBuildsData[buildIdx];

    // Helper to render basic items (no win rate)
    const renderBasicItems = (items) => {
        if (items && items.length > 0) {
            return items.map(item => `
                <div class="item-slot">
                    <img class="item-icon" src="${item.iconURL}" alt="${item.name}" title="${item.name}" />
                </div>
            `).join('');
        }
        return '<div class="items-empty">No data</div>';
    };

    // Helper to render items with win rate
    const renderItemsWithWR = (items) => {
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
    };

    buildContent.innerHTML = `
        <div class="items-section">
            <div class="items-header">Starting Items</div>
            <div class="items-grid">${renderBasicItems(build.startingItems)}</div>
        </div>
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

// Show tabs on startup and try to load meta data
setTimeout(() => {
    tabsContainer.classList.remove('hidden');
    statusCard.classList.add('hidden');
}, 500);
