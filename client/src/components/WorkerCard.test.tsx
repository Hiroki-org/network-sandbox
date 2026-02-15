import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import WorkerCard from './WorkerCard';
import { Worker, WorkerConfig } from '../types';

const mockWorker: Worker = {
  id: 'worker-1',
  name: 'test-worker',
  url: 'http://localhost:8001',
  color: '#3B82F6',
  weight: 1,
  maxLoad: 10,
  healthy: true,
  currentLoad: 2,
  enabled: true,
  totalRequests: 100,
  failedRequests: 0,
  circuitOpen: false,
};

const mockConfig: WorkerConfig = {
  max_concurrent_requests: 10,
  response_delay_ms: 50,
  failure_rate: 0.1,
  queue_size: 100,
};

describe('WorkerCard Component', () => {
  const mockToggle = jest.fn();
  const mockUpdateWeight = jest.fn();
  const mockUpdateConfig = jest.fn();
  const mockToggleExpand = jest.fn();

  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('renders worker information correctly', () => {
    render(
      <WorkerCard
        worker={mockWorker}
        config={mockConfig}
        isExpanded={false}
        onToggle={mockToggle}
        onUpdateWeight={mockUpdateWeight}
        onUpdateConfig={mockUpdateConfig}
        onToggleExpand={mockToggleExpand}
      />
    );

    expect(screen.getByText('test-worker')).toBeInTheDocument();
    expect(screen.getByText('2/10')).toBeInTheDocument();
    expect(screen.getByText('容量: 10')).toBeInTheDocument();
  });

  it('calls onToggle when toggle button is clicked', () => {
    render(
      <WorkerCard
        worker={mockWorker}
        config={mockConfig}
        isExpanded={false}
        onToggle={mockToggle}
        onUpdateWeight={mockUpdateWeight}
        onUpdateConfig={mockUpdateConfig}
        onToggleExpand={mockToggleExpand}
      />
    );

    const toggleButton = screen.getByTitle('無効にする');
    fireEvent.click(toggleButton);

    expect(mockToggle).toHaveBeenCalledWith('test-worker', false);
  });

  it('calls onToggleExpand when expand button is clicked', () => {
    render(
      <WorkerCard
        worker={mockWorker}
        config={mockConfig}
        isExpanded={false}
        onToggle={mockToggle}
        onUpdateWeight={mockUpdateWeight}
        onUpdateConfig={mockUpdateConfig}
        onToggleExpand={mockToggleExpand}
      />
    );

    const expandButton = screen.getByText('▼ 設定を開く');
    fireEvent.click(expandButton);

    expect(mockToggleExpand).toHaveBeenCalledWith('test-worker');
  });

  it('renders config panel when isExpanded is true', () => {
    render(
      <WorkerCard
        worker={mockWorker}
        config={mockConfig}
        isExpanded={true}
        onToggle={mockToggle}
        onUpdateWeight={mockUpdateWeight}
        onUpdateConfig={mockUpdateConfig}
        onToggleExpand={mockToggleExpand}
      />
    );

    expect(screen.getByText('▲ 設定を閉じる')).toBeInTheDocument();
    expect(screen.getByText(/同時リクエスト数:/)).toBeInTheDocument();
  });
});
