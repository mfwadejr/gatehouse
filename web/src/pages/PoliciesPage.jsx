import React from 'react';
import { useHashTab } from '../hashtab.js';
import { Page, Tabs } from '../ui.jsx';
import Policies from './Policies.jsx';
import Routes from './Routes.jsx';

// Everything that decides where a device's traffic may go, on one page: the list of every policy in
// plain words, and the routes that send traffic through a VPN or a chosen connection. Routes used to be
// a page of its own; #/routes still lands here.
export default function PoliciesPage() {
  const [tab, setTab] = useHashTab('policies', ['all', 'routes'], 'all');
  return (
    <Page title="Policies" sub="Everything that allows, blocks, routes, opens or speeds something for your devices, in plain words.">
      <Tabs value={tab} onChange={setTab} tabs={[{ value: 'all', label: 'All policies' }, { value: 'routes', label: 'Routes' }]} />
      {tab === 'all' && <Policies part />}
      {tab === 'routes' && <Routes part />}
    </Page>
  );
}
