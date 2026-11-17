import { fireEvent, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { expect, it, vi } from 'vitest';

import InboundList from '@/pages/inbounds/list/InboundList';
import type { DBInboundRecord } from '@/pages/inbounds/list/types';
import type { NodeRecord } from '@/api/queries/useNodesQuery';

import { renderWithProviders } from './test-utils';

function row(id: number, nodeId: number | null, linkGroup = ''): DBInboundRecord {
  return {
    id,
    enable: true,
    remark: `edge-${id}`,
    subSortIndex: 1,
    port: 443,
    protocol: 'vless',
    up: 0,
    down: 0,
    total: 0,
    expiryTime: 0,
    _expiryTime: null,
    nodeId,
    linkGroup,
    settings: '{"clients":[]}',
    streamSettings: '{"network":"tcp","security":"none"}',
    isVLess: true,
  };
}

// Copies of one inbound say how many there are, a copy left alone does not, and only a
// copy offers to unlink.
it('marks linked copies and offers to unlink them', () => {
  const onRowAction = vi.fn();
  const nodes = new Map([
    [2, { id: 2, name: 'fra', status: 'online', enable: true } as unknown as NodeRecord],
  ]);
  renderWithProviders(
    <MemoryRouter>
      <InboundList
        dbInbounds={[row(1, null, 'g1'), row(2, 2, 'g1'), row(3, null), row(4, 2, 'gone')]}
        clientCount={{}}
        onlineClients={[]}
        lastOnlineMap={{}}
        inboundSpeed={{}}
        expireDiff={0}
        trafficDiff={0}
        pageSize={20}
        isMobile={false}
        subEnable={false}
        nodesById={nodes}
        hasActiveNode
        hosts={[]}
        onAddInbound={vi.fn()}
        onGeneralAction={vi.fn()}
        onRowAction={onRowAction}
        onBulkDelete={vi.fn()}
      />
    </MemoryRouter>,
  );
  expect(screen.getAllByText('2 linked')).toHaveLength(2);
  expect(screen.queryByText('1 linked')).toBeNull();
  const openMenu = (id: number) => {
    const more = document.querySelector(
      `tr[data-row-key="${id}"] .action-buttons button:last-child`,
    );
    if (!more) throw new Error(`row ${id} has no actions menu`);
    fireEvent.click(more);
  };
  openMenu(3);
  expect(screen.queryByText('Unlink from its copies')).toBeNull();
  openMenu(1);
  fireEvent.click(screen.getByText('Unlink from its copies'));
  expect(onRowAction).toHaveBeenCalledWith(
    expect.objectContaining({ key: 'unlink', dbInbound: expect.objectContaining({ id: 1 }) }),
  );
});
